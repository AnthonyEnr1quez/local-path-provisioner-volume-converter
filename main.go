package main

import (
	"errors"
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/prompt"
	"github.com/samber/lo"
)

func main() {
	fmt.Print("Use \"Ctrl+C\" to quit\n\n")

	for {
		cw := kube.GetClientWrapper()

		selectedNamespace, selectedCharts, err := selectNamespace(&cw)
		if err != nil {
			log.Fatalln(err)
		}

		volumes, selectedChart, selectedChartName, err := selectChart(&cw, selectedCharts)
		if err != nil {
			fmt.Println(err.Error() + "\n")
			continue
		}

		volume, volumeName, err := selectVolume(volumes)
		if err != nil {
			log.Fatalln(err)
		}

		pvcName := volume.Spec.ClaimRef.Name
		pvcNamespace := volume.Spec.ClaimRef.Namespace

		exec(cw, selectedChart, pvcName, selectedNamespace, selectedChartName, volumeName, pvcNamespace)
	}
}

func exec(cw kube.ClientWrapper, selectedChart unstructured.Unstructured, pvcName, selectedNamespace, selectedChartName, volumeName, pvcNamespace string) {
	err := cw.CreateNamespace(kube.MigrationNamespace)
	if err != nil {
		log.Fatalln(err)
	}

	err = cw.CreateServiceAccount(kube.MigrationNamespace, kube.MigrationServiceAccount)
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Printf("\nConverting PVC %s from host path volume to local volume\n\n", pvcName)

	patchy, err := kube.NewPatcher(selectedChart)
	if err != nil {
		log.Fatalln(err)
	}

	tempPVCName, err := cw.AddTempPVC(patchy, selectedNamespace, selectedChartName, volumeName)
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

	jobName, err := cw.MigrateJob(pvcNamespace, pvcName, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsJobFinished(kube.MigrationNamespace, jobName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UpdateOriginalPVC(patchy, selectedNamespace, selectedChartName, volumeName)
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

	jobName, err = cw.MigrateJob(pvcNamespace, tempPVCName, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = kube.WaitFor(cw.IsJobFinished(kube.MigrationNamespace, jobName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UnbindTempPVC(patchy, selectedNamespace, selectedChartName, volumeName)
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

	fmt.Printf("\nPVC %s converted\n\n", pvcName)

	fmt.Print("Make sure to add the following block to the PVC declaration of your resource definition file if used.\n\n")
	fmt.Print("annotations: \n  volumeType: local\n\n")
}

func selectNamespace(cw *kube.ClientWrapper) (string, []unstructured.Unstructured, error) {
	namespaces, err := cw.GetNamespaces()
	if err != nil {
		return "", nil, err
	}

	helmChartsByNamespace := lo.Associate(namespaces, func(n corev1.Namespace) (string, []unstructured.Unstructured) {
		helmReleases, err := cw.GetResourceList(n.Name, kube.FluxHelmReleaseResource)
		if err != nil && !apierrors.IsNotFound(err) {
			return "", nil
		}
		helmCharts, err := cw.GetResourceList(n.Name, kube.HelmChartResource)
		if err != nil && !apierrors.IsNotFound(err) {
			return "", nil
		}

		helmCharts = append(helmCharts, helmReleases...)
		if len(helmCharts) == 0 {
			return "", nil
		}

		return n.Name, helmCharts
	})

	filtered := lo.OmitByKeys(helmChartsByNamespace, []string{""})
	if len(filtered) == 0 {
		return "", nil, errors.New("No namespaces that have supported resources")
	}

	selectedNamespace, err := prompt.AskOne(
		"Select namespace",
		lo.Keys(filtered),
		func(value string, index int) string {
			count := len(filtered[value])
			return fmt.Sprintf("%d possible resources", count)
		},
	)

	return selectedNamespace, filtered[selectedNamespace], err
}

func selectChart(cw *kube.ClientWrapper, charts []unstructured.Unstructured) ([]*corev1.PersistentVolume, unstructured.Unstructured, string, error) {
	helmChartByName := lo.KeyBy(charts, func(chart unstructured.Unstructured) string {
		chartName, found, err := unstructured.NestedString(chart.UnstructuredContent(), "metadata", "name")
		if err != nil || !found {
			return ""
		}

		return chartName
	})

	pvsByHelmChartName := lo.Associate(charts, func(chart unstructured.Unstructured) (string, []*corev1.PersistentVolume) {
		chartName, found, err := unstructured.NestedString(chart.UnstructuredContent(), "metadata", "name")
		if err != nil || !found {
			return "", nil
		}
		var namespace string

		var path []string
		switch chart.GetKind() {
		case "HelmRelease":
			path = []string{"metadata", "namespace"}
		case "HelmChart":
			path = []string{"spec", "targetNamespace"}
		}

		namespace, found, err = unstructured.NestedString(chart.UnstructuredContent(), path...)
		if err != nil || !found {
			return "", nil
		}

		pvcs, _ := cw.GetPVCsByChartName(namespace, chartName)
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

		if len(volumesToUpdate) == 0 {
			return "", nil
		}

		return chartName, volumesToUpdate
	})

	filtered := lo.OmitByKeys(pvsByHelmChartName, []string{""})
	if len(filtered) == 0 {
		return nil, unstructured.Unstructured{}, "", errors.New("No charts that have host path volumes")
	}

	selectedChart, err := prompt.AskOne(
		"Select Chart",
		lo.Keys(filtered),
		func(value string, index int) string {
			count := len(filtered[value])
			return fmt.Sprintf("%d host path volumes", count)
		},
	)

	return filtered[selectedChart], helmChartByName[selectedChart], selectedChart, err
}

func selectVolume(volumes []*corev1.PersistentVolume) (*corev1.PersistentVolume, string, error) {
	volsByPVCName := lo.Associate(volumes, func(v *corev1.PersistentVolume) (string, *corev1.PersistentVolume) {
		return v.Spec.ClaimRef.Name, v
	})

	selectedVolumeName, err := prompt.AskOne("Select Volume", lo.Keys(volsByPVCName), nil)

	return volsByPVCName[selectedVolumeName], selectedVolumeName[strings.IndexByte(selectedVolumeName, '-')+1:], err
}
