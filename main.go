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

	selectedNS, selectedCharts, err := selectNamespace(&cw)
	if err != nil {
		log.Fatalln(err)
	}

	volumes, selectedChart := selectChart(&cw, selectedCharts)

	volume, volumeName := selectVolume(volumes)

	pvcName := volume.Spec.ClaimRef.Name

	podNS := volume.Spec.ClaimRef.Namespace

	fmt.Println("Doing stuff to this pvc:", pvcName)

	tempPVCName, err := cw.AddTempPVC(selectedNS, selectedChart, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPVCBound(podNS, tempPVCName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPodReady(podNS, selectedChart))
	if err != nil {
		log.Fatalln(err.Error())
	}
	fmt.Println("pod ready after first bind")

	err = cw.ScaleDeployment(podNS, selectedChart, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.PvMigrater(podNS, pvcName, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(podNS, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UpdateOriginalPVC(selectedNS, selectedChart, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPVCBound(podNS, tempPVCName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPodReady(podNS, selectedChart))
	if err != nil {
		log.Fatalln(err.Error())
	}
	fmt.Println("pod ready after second bind")

	err = cw.ScaleDeployment(podNS, selectedChart, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.PvMigrater(podNS, tempPVCName, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UnbindTempPVC(selectedNS, selectedChart, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(podNS, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsPodReady(podNS, selectedChart))
	if err != nil {
		log.Fatalln(err.Error())
	}
	fmt.Println("pod ready after third bind")

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

	selectedNS := prompt.AskOne(
		"Select namespace",
		lo.Keys(filtered),
		func(value string, index int) string {
			return strconv.Itoa(len(filtered[value]))
		},
	)

	return selectedNS, filtered[selectedNS], nil
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
	volsByPVCName := lo.Associate(volumes, func(v *corev1.PersistentVolume) (string, *corev1.PersistentVolume){
		return v.Spec.ClaimRef.Name, v
	})

	selectedVolumeName := prompt.AskOne("Select Volume", lo.Keys(volsByPVCName), nil)

	// pvcNames := lo.Map(volumes, func(vol *corev1.PersistentVolume, _ int) string {
	// 	return vol.Spec.ClaimRef.Name
	// })

	// selectedVolumeName := prompt.AskOne("Select Volume", pvcNames, nil)

	return volsByPVCName[selectedVolumeName], selectedVolumeName[strings.IndexByte(selectedVolumeName, '-')+1:]
}
