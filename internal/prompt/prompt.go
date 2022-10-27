package prompt

import (
	"errors"
	"fmt"

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

func Survey(cw kube.ClientWrapper) (resourceNamespace, resourceName string, volume *corev1.PersistentVolume, patcher kube.Patcher, err error) {
	resourceNamespace, resources, err := selectNamespace(&cw)
	if err != nil {
		return
	}

	resourceName, volumes, patcher, err := selectResource(&cw, resources)
	if err != nil {
		return
	}

	volume, err = selectVolume(volumes)

	return
}

func selectNamespace(cw *kube.ClientWrapper) (string, []unstructured.Unstructured, error) {
	namespaces, err := cw.GetNamespaces()
	if err != nil {
		return "", nil, err
	}

	resourcesByNamespace := lo.Associate(namespaces, func(n corev1.Namespace) (string, []unstructured.Unstructured) {
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

	filteredResources := lo.OmitByKeys(resourcesByNamespace, []string{""})
	if len(filteredResources) == 0 {
		return "", nil, errors.New("No namespaces that have supported resources")
	}

	resourceNamespace, err := askOne(
		"Select namespace",
		lo.Keys(filteredResources),
		func(value string, _ int) string {
			count := len(filteredResources[value])
			return fmt.Sprintf("%d possible resources", count)
		},
	)

	return resourceNamespace, filteredResources[resourceNamespace], err
}

func selectResource(cw *kube.ClientWrapper, resources []unstructured.Unstructured) (string, []*corev1.PersistentVolume, kube.Patcher, error) {
	pvsByResourceName := lo.Associate(resources, func(resource unstructured.Unstructured) (string, lo.Tuple2[[]*corev1.PersistentVolume, kube.Patcher]) {
		name, found, err := unstructured.NestedString(resource.UnstructuredContent(), "metadata", "name")
		if err != nil || !found {
			return "", lo.T2[[]*corev1.PersistentVolume, kube.Patcher](nil, nil)
		}

		patcher, err := kube.NewPatcher(resource.GetKind())
		if err != nil {
			return "", lo.T2[[]*corev1.PersistentVolume, kube.Patcher](nil, nil)
		}

		namespace, found, err := unstructured.NestedString(resource.UnstructuredContent(), patcher.GetNamespacePath()...)
		if err != nil || !found {
			return "", lo.T2[[]*corev1.PersistentVolume, kube.Patcher](nil, nil)
		}

		pvcs, err := cw.GetPVCsByResourceName(namespace, name)
		if err != nil {
			return "", lo.T2[[]*corev1.PersistentVolume, kube.Patcher](nil, nil)
		}

		volumesToUpdate := lo.FilterMap(pvcs, func(pvc corev1.PersistentVolumeClaim, _ int) (*corev1.PersistentVolume, bool) {
			pv, err := cw.GetPVByName(pvc.Spec.VolumeName)
			if err != nil {
				return nil, false
			}

			if pv.Spec.PersistentVolumeSource.HostPath != nil {
				return pv, true
			}

			return nil, false
		})

		if len(volumesToUpdate) == 0 {
			return "", lo.T2[[]*corev1.PersistentVolume, kube.Patcher](nil, nil)
		}

		return name, lo.T2(volumesToUpdate, patcher)
	})

	filteredPVs := lo.OmitByKeys(pvsByResourceName, []string{""})
	if len(filteredPVs) == 0 {
		return "", nil, nil, errors.New("No resources that have host path volumes")
	}

	selectedResourceName, err := askOne(
		"Select Resource",
		lo.Keys(filteredPVs),
		func(value string, _ int) string {
			count := len(filteredPVs[value].A)
			return fmt.Sprintf("%d host path volumes", count)
		},
	)

	return selectedResourceName, filteredPVs[selectedResourceName].A, filteredPVs[selectedResourceName].B, err
}

func selectVolume(volumes []*corev1.PersistentVolume) (*corev1.PersistentVolume, error) {
	volsByPVCName := lo.Associate(volumes, func(v *corev1.PersistentVolume) (string, *corev1.PersistentVolume) {
		return v.Spec.ClaimRef.Name, v
	})

	selectedVolumeName, err := askOne("Select Volume", lo.Keys(volsByPVCName), nil)

	return volsByPVCName[selectedVolumeName], err
}
