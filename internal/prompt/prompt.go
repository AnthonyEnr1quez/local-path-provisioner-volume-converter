package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func askOne(msg string, options []string, description func(value string, index int) string) (answer string, err error) {
	prompt := &survey.Select{
		Message:     msg,
		Options:     options,
		Description: description,
	}
	err = survey.AskOne(prompt, &answer)
	return
}

func Survey(cw kube.ClientWrapper) (selectedChart unstructured.Unstructured, pvcName, selectedNamespace, selectedChartName, volumeName, pvcNamespace, volumeSize string, err error) {
	selectedNamespace, selectedCharts, err := selectNamespace(&cw)
	if err != nil {
		return
	}

	volumes, selectedChart, selectedChartName, err := selectChart(&cw, selectedCharts)
	if err != nil {
		return
	}

	volume, volumeName, err := selectVolume(volumes)
	if err != nil {
		return
	}

	pvcName = volume.Spec.ClaimRef.Name
	pvcNamespace = volume.Spec.ClaimRef.Namespace
	volumeSize = volume.Spec.Capacity.Storage().String()

	return
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

	selectedNamespace, err := askOne(
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

	selectedChart, err := askOne(
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

	selectedVolumeName, err := askOne("Select Volume", lo.Keys(volsByPVCName), nil)

	return volsByPVCName[selectedVolumeName], selectedVolumeName[strings.IndexByte(selectedVolumeName, '-')+1:], err
}
