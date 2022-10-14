package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/prompt"
	"github.com/samber/lo"
)

/// TODO just throw it all in while loop until they quit
func main() {
	err := kube.CreateTempFiles()
	if err != nil {
		log.Fatalln(err.Error())
	}

	defer func() {
		err = kube.CleanUpTempFiles()
		if err != nil {
			log.Fatalln(err.Error())
		}
	}()

	cw := kube.GetClientWrapper()

	selectedNamespace, selectedCharts, err := selectNamespace(&cw)
	if err != nil {
		log.Fatalln(err)
	}

	volumes, selectedChartName := selectChart(&cw, selectedCharts)

	volume, volumeName := selectVolume(volumes)

	pvcName := volume.Spec.ClaimRef.Name
	pvcNamespace := volume.Spec.ClaimRef.Namespace

	fmt.Printf("\nUpdating PVC %s from host path volume to local volume\n\n", pvcName)

	tempPVCName, err := cw.AddTempPVC(selectedNamespace, selectedChartName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPVCBound(pvcNamespace, tempPVCName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPodReady(pvcNamespace, selectedChartName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.ScaleDeployment(pvcNamespace, selectedChartName, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.PvMigrater(pvcNamespace, pvcName, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UpdateOriginalPVC(selectedNamespace, selectedChartName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPVCBound(pvcNamespace, pvcName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPodReady(pvcNamespace, selectedChartName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.ScaleDeployment(pvcNamespace, selectedChartName, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.PvMigrater(pvcNamespace, tempPVCName, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UnbindTempPVC(selectedNamespace, selectedChartName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPodReady(pvcNamespace, selectedChartName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	fmt.Println("DONE")
}

func selectNamespace(cw *kube.ClientWrapper) (chartNamespace string, charts []unstructured.Unstructured, err error) {
	namespaces, err := cw.GetNamespaces()
	if err != nil {
		return "", nil, err
	}

	helmChartsByNamespace := lo.Associate(namespaces, func(n corev1.Namespace) (string, []unstructured.Unstructured) {
		charts, err := cw.GetHelmCharts(n.Name)
		if err != nil || len(charts) == 0 {
			return "", nil
		}

		return n.Name, charts
	})

	filtered := lo.OmitByKeys(helmChartsByNamespace, []string{""})

	selectedNamespace := prompt.AskOne(
		"Select namespace",
		lo.Keys(filtered),
		func(value string, index int) string {
			return strconv.Itoa(len(filtered[value]))
		},
	)

	return selectedNamespace, filtered[selectedNamespace], nil
}

func selectChart(cw *kube.ClientWrapper, charts []unstructured.Unstructured) ([]*corev1.PersistentVolume, string) {
	pvsByHelmChartName := lo.Associate(charts, func(chart unstructured.Unstructured) (string, []*corev1.PersistentVolume) {
		// TODO this is temp for helm chart, will have to update for flux parsing
		if chart.Object["spec"].(map[string]interface{})["targetNamespace"] != nil {
			chartName := chart.Object["metadata"].(map[string]interface{})["name"].(string)
			targetNamespace := chart.Object["spec"].(map[string]interface{})["targetNamespace"].(string)

			pvcs, _ := cw.GetPVCsByChartName(targetNamespace, chartName)
			volumesToUpdate := lo.FilterMap(pvcs, func(pvc corev1.PersistentVolumeClaim, _ int) (*corev1.PersistentVolume, bool) {
				pv, err := cw.GetPVFromPVC(&pvc)
				if err != nil {
					return nil, false
				}

				if pv.Spec.PersistentVolumeSource.HostPath != nil {
					return pv, true
				}

				return nil, false
			})

			return chartName, volumesToUpdate
		}

		return "", nil
	})

	filtered := lo.OmitByKeys(pvsByHelmChartName, []string{""})

	selectedChart := prompt.AskOne(
		"Select Chart",
		lo.Keys(filtered),
		func(value string, index int) string {
			return strconv.Itoa(len(filtered[value]))
		},
	)

	return filtered[selectedChart], selectedChart
}

func selectVolume(volumes []*corev1.PersistentVolume) (*corev1.PersistentVolume, string) {
	volsByPVCName := lo.Associate(volumes, func(v *corev1.PersistentVolume) (string, *corev1.PersistentVolume) {
		return v.Spec.ClaimRef.Name, v
	})

	selectedVolumeName := prompt.AskOne("Select Volume", lo.Keys(volsByPVCName), nil)

	return volsByPVCName[selectedVolumeName], selectedVolumeName[strings.IndexByte(selectedVolumeName, '-')+1:]
}
