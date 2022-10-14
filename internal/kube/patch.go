package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type PersistenceValues struct {
	Persistence map[string]interface{} `yaml:"persistence"`
}

func patchChart(dynamicClient dynamic.Interface, namespace, chartName, pvcName string, patchFunc func(*PersistenceValues, string)) error {
	chartsClient := dynamicClient.Resource(helmChartResource).Namespace(namespace)
	chart, err := chartsClient.Get(context.Background(), chartName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	values, exists, err := unstructured.NestedString(chart.UnstructuredContent(), "spec", "valuesContent")
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(fmt.Sprintf("values not found on helm chart %s", chartName))
	}

	var persistenceVals PersistenceValues
	yaml.Unmarshal([]byte(values), &persistenceVals)

	patchFunc(&persistenceVals, pvcName)

	// todo explore empty fields
	outVals, err := yaml.Marshal(persistenceVals)
	if err != nil {
		return err
	}

	patch := []interface{}{
		map[string]interface{}{
			"op":    "replace",
			"path":  "/spec/valuesContent",
			"value": string(outVals),
		},
	}

	payload, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = chartsClient.Patch(context.Background(), chartName, types.JSONPatchType, payload, metav1.PatchOptions{})
	if err != nil {
		return err
	}

	fmt.Println("helm chart patched")
	return nil
}

func (cw *ClientWrapper) AddTempPVC(namespace, chartName, pvcName string) (string, error) {
	tempPVCName := fmt.Sprint(pvcName, "-temp")
	patch := func(p *PersistenceValues, pvcName string) {
		p.Persistence[tempPVCName] = map[interface{}]interface{}{
			"enabled":    true,
			"retain":     true,
			"accessMode": "ReadWriteOnce",
			"size":       "1Gi",
			"annotations": map[interface{}]interface{}{
				"volumeType": "local",
			},
		}
	}
	err := patchChart(cw.dc, namespace, chartName, pvcName, patch)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s-%s", chartName, tempPVCName), nil
}

func (cw *ClientWrapper) UpdateOriginalPVC(namespace, chartName, pvcName string) error {
	patch := func(p *PersistenceValues, pvcName string) {
		p.Persistence[pvcName].(map[string]interface{})["annotations"] = map[string]interface{}{
			"volumeType": "local",
		}
	}

	return patchChart(cw.dc, namespace, chartName, pvcName, patch)
}

func (cw *ClientWrapper) UnbindTempPVC(namespace, chartName, pvcName string) error {
	patch := func(p *PersistenceValues, pvcName string) {
		delete(p.Persistence, fmt.Sprint(pvcName, "-temp"))
	}

	return patchChart(cw.dc, namespace, chartName, pvcName, patch)
}
