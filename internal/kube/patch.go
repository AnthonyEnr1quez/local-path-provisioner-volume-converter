package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type Patcher interface {
	getResource() schema.GroupVersionResource
	getValues(map[string]interface{}, string) (valuesMap, persistence map[string]interface{}, err error)
	getPayload(map[string]interface{}, string) (payload []byte, patchType types.PatchType, err error)
}

type HelmChartPatcher struct{}

func (hcp HelmChartPatcher) getResource() schema.GroupVersionResource {
	return HelmChartResource
}

func (hcp HelmChartPatcher) getValues(uc map[string]interface{}, chartName string) (valuesMap, persistence map[string]interface{}, err error) {
	valuesContent, found, err := unstructured.NestedString(uc, "spec", "valuesContent")
	if err != nil {
		return
	}
	if !found {
		err = errors.New(fmt.Sprintf("valuesContent not found on helm chart %s", chartName))
		return
	}

	err = yaml.Unmarshal([]byte(valuesContent), &valuesMap)
	if err != nil {
		return
	}

	if val, ok := valuesMap["persistence"]; ok {
		persistence = val.(map[string]interface{})
	} else {
		err = errors.New(fmt.Sprintf("persistence values not found on helm chart %s", chartName))
		return
	}

	return
}

func (hcp HelmChartPatcher) getPayload(vals map[string]interface{}, pvcName string) (payload []byte, patchType types.PatchType, err error) {
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

func (hrp HelmReleasePatcher) getResource() schema.GroupVersionResource {
	return FluxHelmReleaseResource
}

func (hrp HelmReleasePatcher) getValues(uc map[string]interface{}, chartName string) (valuesMap, persistence map[string]interface{}, err error) {
	valuesMap, found, err := unstructured.NestedMap(uc, "spec", "values")
	if err != nil {
		return
	}
	if !found {
		err = errors.New(fmt.Sprintf("valuesContent not found on helm chart %s", chartName))
		return
	}

	if val, ok := valuesMap["persistence"]; ok {
		persistence = val.(map[string]interface{})
	} else {
		err = errors.New(fmt.Sprintf("persistence values not found on helm chart %s", chartName))
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
		err = errors.New("persistence values not found on helm chart")
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

func NewPatcher(chart unstructured.Unstructured) (Patcher, error) {
	switch chart.GetKind() {
	case "HelmChart":
		return HelmChartPatcher{}, nil
	case "HelmRelease":
		return HelmReleasePatcher{}, nil
	default:
		return nil, nil
	}
}

func patchChart(patchy Patcher, dynamicClient dynamic.Interface, namespace string, chartIn unstructured.Unstructured, chartName, pvcName string, patchFunc func(map[string]interface{}, string), tempDeleteFlag bool) error {
	chartsClient := dynamicClient.Resource(patchy.getResource()).Namespace(namespace)
	chart, err := chartsClient.Get(context.Background(), chartName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	values, persistence, err := patchy.getValues(chart.UnstructuredContent(), chartName)

	patchFunc(persistence, pvcName)

	payload, patchType, err := patchy.getPayload(values, pvcName)
	if err != nil {
		return err
	}

	_, err = chartsClient.Patch(context.Background(), chartName, patchType, payload, metav1.PatchOptions{})
	if err != nil {
		return err
	}

	fmt.Println("Helm chart patched")
	return nil
}

func (cw *ClientWrapper) AddTempPVC(patchy Patcher, namespace string, chart unstructured.Unstructured, chartName, pvcName string) (string, error) {
	tempPVCName := fmt.Sprint(pvcName, "-temp")
	patch := func(p map[string]interface{}, pvcName string) {
		p[tempPVCName] = map[string]interface{}{
			"enabled":    true,
			"retain":     true,
			"accessMode": "ReadWriteOnce",
			"size":       "1Gi",
			"annotations": map[string]interface{}{
				"volumeType": "local",
			},
		}
	}
	err := patchChart(patchy, cw.dc, namespace, chart, chartName, pvcName, patch, false)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s-%s", chartName, tempPVCName), nil
}

func (cw *ClientWrapper) UpdateOriginalPVC(patchy Patcher, namespace string, chart unstructured.Unstructured, chartName, pvcName string) error {
	patch := func(p map[string]interface{}, pvcName string) {
		p[pvcName].(map[string]interface{})["annotations"] = map[string]interface{}{
			"volumeType": "local",
		}
	}

	return patchChart(patchy, cw.dc, namespace, chart, chartName, pvcName, patch, false)
}

func (cw *ClientWrapper) UnbindTempPVC(patchy Patcher, namespace string, chart unstructured.Unstructured, chartName, pvcName string) error {
	patch := func(p map[string]interface{}, pvcName string) {
		delete(p, pvcName)
	}

	return patchChart(patchy, cw.dc, namespace, chart, chartName, fmt.Sprint(pvcName, "-temp"), patch, true)
}

// TODO
// func patchCall[T Patcher](patcher T, cc dynamic.ResourceInterface, vals map[string]interface{}, chartName string, pvcName string) error {
// 	payload, patchType := patcher.getPayload(vals, pvcName)
// 	_, err := cc.Patch(context.Background(), chartName, patchType, payload, metav1.PatchOptions{})
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }
