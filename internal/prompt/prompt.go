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

func Survey(cw kube.ClientWrapper) (resourceType, resourceNamespace, resourceName string, volume *corev1.PersistentVolume, err error) {
	resourceNamespace, resources, err := selectNamespace(&cw)
	if err != nil {
		return
	}

	resourceType, resourceName, volumes, err := selectResource(&cw, resources)
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
		func(value string, index int) string {
			count := len(filteredResources[value])
			return fmt.Sprintf("%d possible resources", count)
		},
	)

	return resourceNamespace, filteredResources[resourceNamespace], err
}

func selectResource(cw *kube.ClientWrapper, resources []unstructured.Unstructured) (string, string, []*corev1.PersistentVolume, error) {
	getResourceName := func(resource unstructured.Unstructured) (name string) {
		name, _, _ = unstructured.NestedString(resource.UnstructuredContent(), "metadata", "name")
		return
	}

	resourceByName := lo.KeyBy(resources, getResourceName)

	pvsByResourceName := lo.Associate(resources, func(resource unstructured.Unstructured) (string, []*corev1.PersistentVolume) {
		name := getResourceName(resource)

		var path []string
		switch resource.GetKind() {
		// todo
		case "HelmRelease":
			path = []string{"metadata", "namespace"}
		case "HelmChart":
			path = []string{"spec", "targetNamespace"}
		}

		namespace, found, err := unstructured.NestedString(resource.UnstructuredContent(), path...)
		if err != nil || !found {
			return "", nil
		}

		pvcs, _ := cw.GetPVCsByResourceName(namespace, name)
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
			return "", nil
		}

		return name, volumesToUpdate
	})

	filteredPVs := lo.OmitByKeys(pvsByResourceName, []string{""})
	if len(filteredPVs) == 0 {
		return "", "", nil, errors.New("No resources that have host path volumes")
	}

	selectedResourceName, err := askOne(
		"Select Resource",
		lo.Keys(filteredPVs),
		func(value string, index int) string {
			count := len(filteredPVs[value])
			return fmt.Sprintf("%d host path volumes", count)
		},
	)

	selectedResource := resourceByName[selectedResourceName]

	return selectedResource.GetKind(), selectedResourceName, filteredPVs[selectedResourceName], err
}

func selectVolume(volumes []*corev1.PersistentVolume) (*corev1.PersistentVolume, error) {
	volsByPVCName := lo.Associate(volumes, func(v *corev1.PersistentVolume) (string, *corev1.PersistentVolume) {
		return v.Spec.ClaimRef.Name, v
	})

	selectedVolumeName, err := askOne("Select Volume", lo.Keys(volsByPVCName), nil)

	return volsByPVCName[selectedVolumeName], err
}
