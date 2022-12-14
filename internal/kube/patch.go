package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type Patcher interface {
	GetNamespacePath() []string
	getResource() schema.GroupVersionResource
	getValues(map[string]interface{}, string) (valuesMap, persistence map[string]interface{}, err error)
	getPayload(map[string]interface{}, string) (payload []byte, patchType types.PatchType, err error)
}

type HelmChartPatcher struct{}

func (hcp HelmChartPatcher) GetNamespacePath() []string {
	return []string{"spec", "targetNamespace"}
}

func (hcp HelmChartPatcher) getResource() schema.GroupVersionResource {
	return HelmChartResource
}

func (hcp HelmChartPatcher) getValues(uc map[string]interface{}, chartName string) (valuesMap, persistence map[string]interface{}, err error) {
	valuesContent, found, err := unstructured.NestedString(uc, "spec", "valuesContent")
	if err != nil {
		return
	}
	if !found {
		err = errors.New(fmt.Sprintf("valuesContent not found on resource %s", chartName))
		return
	}

	err = yaml.Unmarshal([]byte(valuesContent), &valuesMap)
	if err != nil {
		return
	}

	if val, ok := valuesMap["persistence"]; ok {
		persistence = val.(map[string]interface{})
	} else {
		err = errors.New(fmt.Sprintf("persistence values not found on resource %s", chartName))
		return
	}

	return
}

func (hcp HelmChartPatcher) getPayload(vals map[string]interface{}, _ string) (payload []byte, patchType types.PatchType, err error) {
	yaml, err := yaml.Marshal(vals)
	if err != nil {
		return
	}

	patch := []interface{}{
		map[string]interface{}{
			"op":    "replace",
			"path":  "/spec/valuesContent",
			"value": string(yaml),
		},
	}

	payload, err = json.Marshal(patch)
	if err != nil {
		return
	}

	return payload, types.JSONPatchType, err
}

type HelmReleasePatcher struct{}

func (hrp HelmReleasePatcher) GetNamespacePath() []string {
	return []string{"metadata", "namespace"}
}

func (hrp HelmReleasePatcher) getResource() schema.GroupVersionResource {
	return FluxHelmReleaseResource
}

func (hrp HelmReleasePatcher) getValues(uc map[string]interface{}, chartName string) (valuesMap, persistence map[string]interface{}, err error) {
	valuesMap, found, err := unstructured.NestedMap(uc, "spec", "values")
	if err != nil {
		return
	}
	if !found {
		err = errors.New(fmt.Sprintf("valuesContent not found on resource %s", chartName))
		return
	}

	if val, ok := valuesMap["persistence"]; ok {
		persistence = val.(map[string]interface{})
	} else {
		err = errors.New(fmt.Sprintf("persistence values not found on resource %s", chartName))
		return
	}

	return
}

func (hrp HelmReleasePatcher) getPayload(vals map[string]interface{}, pvcName string) (payload []byte, patchType types.PatchType, err error) {
	persistence, found, err := unstructured.NestedMap(vals, "persistence")
	if err != nil {
		return
	}
	if !found {
		err = errors.New("persistence values not found on resource")
		return
	}

	// use json patch for delete op of temp pvc
	if !strings.Contains(pvcName, "-temp") {
		json, err := json.Marshal(persistence)
		if err != nil {
			return nil, "", err
		}

		payload = []byte(fmt.Sprintf(`{"spec": {"values":{"persistence": %s}}}`, json))
		patchType = types.MergePatchType
	} else {
		patch := []interface{}{
			map[string]interface{}{
				"op":   "remove",
				"path": fmt.Sprintf("/spec/values/persistence/%s", pvcName),
			},
		}

		payload, err = json.Marshal(patch)
		if err != nil {
			return
		}
		patchType = types.JSONPatchType
	}

	return
}

func NewPatcher(resourceType string) (Patcher, error) {
	switch resourceType {
	case "HelmChart":
		return HelmChartPatcher{}, nil
	case "HelmRelease":
		return HelmReleasePatcher{}, nil
	default:
		return nil, nil
	}
}

func patchChart(patcher Patcher, dynamicClient dynamic.Interface, namespace, chartName, pvcName string, patchFunc func(map[string]interface{}, string)) error {
	chartsClient := dynamicClient.Resource(patcher.getResource()).Namespace(namespace)
	chart, err := chartsClient.Get(context.Background(), chartName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	values, persistence, err := patcher.getValues(chart.UnstructuredContent(), chartName)

	patchFunc(persistence, pvcName)

	payload, patchType, err := patcher.getPayload(values, pvcName)
	if err != nil {
		return err
	}

	_, err = chartsClient.Patch(context.Background(), chartName, patchType, payload, metav1.PatchOptions{})
	if err != nil {
		return err
	}

	log.Println("Resource patched")
	return nil
}

func (cw *ClientWrapper) AddTempPVC(patcher Patcher, namespace, chartName, pvcName, volumeSize string) (string, error) {
	tempPVCName := fmt.Sprint(pvcName, "-temp")
	patch := func(p map[string]interface{}, pvcName string) {
		p[tempPVCName] = map[string]interface{}{
			"enabled":    true,
			"retain":     true,
			"accessMode": "ReadWriteOnce",
			"size":       volumeSize,
			"annotations": map[string]interface{}{
				"volumeType": "local",
			},
		}
	}
	err := patchChart(patcher, cw.dc, namespace, chartName, pvcName, patch)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s-%s", chartName, tempPVCName), nil
}

func (cw *ClientWrapper) UpdateOriginalPVC(patcher Patcher, namespace, chartName, pvcName string) error {
	patch := func(p map[string]interface{}, pvcName string) {
		p[pvcName].(map[string]interface{})["annotations"] = map[string]interface{}{
			"volumeType": "local",
		}
	}

	return patchChart(patcher, cw.dc, namespace, chartName, pvcName, patch)
}

func (cw *ClientWrapper) UnbindTempPVC(patcher Patcher, namespace, chartName, pvcName string) error {
	patch := func(p map[string]interface{}, pvcName string) {
		delete(p, pvcName)
	}

	return patchChart(patcher, cw.dc, namespace, chartName, fmt.Sprint(pvcName, "-temp"), patch)
}
