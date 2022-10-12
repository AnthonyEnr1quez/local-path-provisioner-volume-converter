package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"

	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	_ "embed"
)

var (
	helmChartResource = schema.GroupVersionResource{
		Group:    "helm.cattle.io",
		Version:  "v1",
		Resource: "helmcharts",
	}

	//go:embed bin/pv-migrate
	pvm []byte
)

type ClientWrapper struct {
	dc dynamic.Interface
	cs kubernetes.Interface
}

func main() {
	config, err := getKubeconfig()
	if err != nil {
		log.Fatalln(err.Error())
	}

	dc, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalln("unable to init dynamic client")
	}

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalln("unable to init clientset")
	}

	cw := ClientWrapper{
		dc: dc,
		cs: cs,
	}

	tempChartNS := "kube-system"
	charts, err := cw.getHelmCharts(tempChartNS)
	if err != nil {
		log.Fatalln(err.Error())
	}

	var sonarrChart unstructured.Unstructured
	var sonarrChartName string
	for _, chart := range charts {
		sonarrChartName = chart.Object["metadata"].(map[string]interface{})["name"].(string)
		if sonarrChartName == "sonarr" {
			sonarrChart = chart
		}
	}

	podNS := sonarrChart.Object["spec"].(map[string]interface{})["targetNamespace"].(string)
	sonarrPod, err := cw.getPodByName(podNS, sonarrChartName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	/// TODO https://pkg.go.dev/os#UserCacheDir
	os.WriteFile("pv-migrate-bin-v1", pvm, 0755)

	for _, vol := range sonarrPod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			pvcName := vol.PersistentVolumeClaim.ClaimName
			pv, err := cw.getPVFromPVCName(podNS, pvcName)
			if err != nil {
				log.Fatalln(err.Error())
			}

			if pv.Spec.PersistentVolumeSource.HostPath != nil {
				fmt.Println("Doing stuff to this pvc:", pvcName)

				writeExtraFile(cs, config, podNS, sonarrPod.Name)

				tempPVCName, err := cw.addTempPVC(tempChartNS, sonarrChartName, vol.Name)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = waitFor(cw.isPVCBound(podNS, tempPVCName))
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = waitFor(cw.isPodReady(cs, podNS, sonarrChartName))
				if err != nil {
					log.Fatalln(err.Error())
				}
				fmt.Println("pod ready after first bind")

				err = cw.scaleDeployment(podNS, sonarrChartName, 0)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = pvMigrater(podNS, pvcName, tempPVCName)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = cw.deletePVC(podNS, pvcName)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = cw.updateOriginalPVC(tempChartNS, sonarrChartName, vol.Name)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = waitFor(cw.isPVCBound(podNS, tempPVCName))
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = waitFor(cw.isPodReady(cs, podNS, sonarrChartName))
				if err != nil {
					log.Fatalln(err.Error())
				}
				fmt.Println("pod ready after second bind")

				err = cw.scaleDeployment(podNS, sonarrChartName, 0)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = pvMigrater(podNS, tempPVCName, pvcName)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = cw.unbindTempPVC(tempChartNS, sonarrChartName, vol.Name)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = cw.deletePVC(podNS, tempPVCName)
				if err != nil {
					log.Fatalln(err.Error())
				}

				err = waitFor(cw.isPodReady(cs, podNS, sonarrChartName))
				if err != nil {
					log.Fatalln(err.Error())
				}
				fmt.Println("pod ready after third bind")

				fmt.Print("asdf")
			}
		}
	}

	os.Remove("pv-migrate-bin-v1")
}

func (cw *ClientWrapper) deletePVC(namespace, name string) error {
	deletePolicy := metav1.DeletePropagationForeground
	err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})

	if err == nil {
		fmt.Println("PVC", name, "deleted")
	}
	return err
}

func pvMigrater(namespace, fromPVC, toPVC string) error {
	cmd := exec.Command("./pv-migrate-bin-v1", "migrate", fromPVC, toPVC, "-n", namespace, "-N", namespace)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError
		}
	}

	if !strings.Contains(out.String(), "Migration succeeded") {
		fmt.Println(out.String())
		return errors.New("pv migration failed")
	}

	fmt.Println(out.String())
	return nil
}

func (cw *ClientWrapper) scaleDeployment(namespace, name string, replicas int) error {
	s, err := cw.cs.AppsV1().Deployments(namespace).GetScale(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	scale := *s
	scale.Spec.Replicas = int32(replicas)

	_, err = cw.cs.AppsV1().Deployments(namespace).UpdateScale(context.Background(), name, &scale, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	err = waitFor(cw.isPodScaled(namespace, name))
	if err != nil {
		return err
	}

	fmt.Println("scaled to 0")
	return nil
}

func getKubeconfig() (*rest.Config, error) {
	configPath, exists := os.LookupEnv("KUBECONFIG")
	if !exists {
		return nil, errors.New("KUBECONFIG env var does not exist")
	}

	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return nil, errors.New("failed to create K8s config")
	}

	return config, nil
}

func (cw *ClientWrapper) getHelmCharts(namespace string) ([]unstructured.Unstructured, error) {
	helmCharts, err := cw.dc.Resource(helmChartResource).Namespace(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return helmCharts.Items, nil
}

type PersistenceValues struct {
	Persistence map[string]interface{} `yaml:"persistence"`
}

func (cw *ClientWrapper) addTempPVC(namespace, chartName, pvcName string) (string, error) {
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

	return "sonarr-" + tempPVCName, nil
}

func (cw *ClientWrapper) updateOriginalPVC(namespace, chartName, pvcName string) error {
	patch := func(p *PersistenceValues, pvcName string) {
		p.Persistence[pvcName].(map[interface{}]interface{})["annotations"] = map[interface{}]interface{}{
			"volumeType": "local",
		}
	}

	return patchChart(cw.dc, namespace, chartName, pvcName, patch)
}

func (cw *ClientWrapper) unbindTempPVC(namespace, chartName, pvcName string) error {
	patch := func(p *PersistenceValues, pvcName string) {
		delete(p.Persistence, fmt.Sprint(pvcName, "-temp"))
	}
	
	return patchChart(cw.dc, namespace, chartName, pvcName, patch)
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

func (cw *ClientWrapper) getPVFromPVCName(namespace, pvcName string) (*corev1.PersistentVolume, error) {
	pvc, err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return cw.cs.CoreV1().PersistentVolumes().Get(context.Background(), pvc.Spec.VolumeName, metav1.GetOptions{})
}

func (cw *ClientWrapper) getPodByName(namespace, name string) (corev1.Pod, error) {
	pods, err := cw.cs.CoreV1().Pods(namespace).List(context.Background(), v1.ListOptions{LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", name)})
	if err != nil {
		return corev1.Pod{}, err
	}

	switch len(pods.Items) {
	case 0:
		return corev1.Pod{}, errors.New(fmt.Sprintf("pod %s not ready yet", name))
	case 1:
		return pods.Items[0], nil
	default:
		return corev1.Pod{}, errors.New(fmt.Sprintf("multiple pods for %s", name))
	}
}

func writeExtraFile(cs kubernetes.Interface, config *rest.Config, namespace, podname string) {
	cmd := []string{
		"/bin/sh",
		"-c",
		"echo \"Hello World!\" > /config/hello.txt",
	}

	req := cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(podname).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: cmd,
		Stdin:   true,
		Stdout:  true,
		Stderr:  true,
		TTY:     true,
	}

	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		fmt.Println(err)
	}
	r, _ := io.Pipe()
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  r,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Tty:    false,
	})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Files Written")
}

func (cw *ClientWrapper) isPodReady(cs kubernetes.Interface, namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Printf(".") // progress bar!

		pod, err := cw.getPodByName(namespace, name)
		if err != nil {
			fmt.Println(err.Error())
			return false, nil
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == "True" {
				return true, nil
			}
		}
		return false, nil
	}
}

func (cw *ClientWrapper) isPodScaled(namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Printf(".") // progress bar!

		_, err := cw.getPodByName(namespace, name)
		if err != nil && strings.Contains(err.Error(), "not ready yet") {
			fmt.Println("scaled")
			return true, nil
		}

		return false, nil
	}
}

func (cw *ClientWrapper) isPVCBound(namespace, pvcName string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Printf(".") // progress bar!

		pvc, err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), pvcName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		switch pvc.Status.Phase {
		case corev1.ClaimBound:
			pv, _ := cw.getPVFromPVCName(namespace, pvcName)
			if pv.Spec.PersistentVolumeSource.Local == nil {
				// TODO handle
				fmt.Printf("%s is not a local volume", pvcName)
			}
			fmt.Println("new pvc bound")
			return true, nil
		default:
			return false, nil
		}
	}
}

func waitFor(condition wait.ConditionFunc) error {
	return wait.PollImmediateInfinite(time.Second, condition)
}

// func getPVCs(cs kubernetes.Interface, namespace string) []corev1.PersistentVolumeClaim {
// 	pvcs, _ := cs.CoreV1().PersistentVolumeClaims(namespace).List(context.Background(), metav1.ListOptions{})
// 	return pvcs.Items
// }

// func getPods(cs kubernetes.Interface) []corev1.Pod {
// 	pods, _ := cs.CoreV1().Pods("test").List(context.Background(), metav1.ListOptions{})
// 	return pods.Items
// }

// func isPodRunning(cs kubernetes.Interface, namespace, name string) wait.ConditionFunc {
// 	return func() (bool, error) {
// 		fmt.Printf(".") // progress bar!

// 		pod, err := getPodByName(cs, namespace, name)
// 		if err != nil {
// 			fmt.Println(err.Error())
// 			return false, nil
// 		}

// 		switch pod.Status.Phase {
// 		case corev1.PodRunning:
// 			fmt.Println("running")
// 			return true, nil
// 		case corev1.PodFailed, corev1.PodSucceeded:
// 			return false, nil // TODO conditions.ErrPodCompleted
// 		}
// 		return false, nil
// 	}
// }

// func isPodDeleted(cs kubernetes.Interface, namespace, name string) wait.ConditionFunc {
// 	return func() (bool, error) {
// 		fmt.Printf(".") // progress bar!

// 		_, err := getPodByName(cs, namespace, name)
// 		if err != nil && err.Error() == fmt.Sprintf("pods \"%s\" not found", name) {
// 			fmt.Println("deleted")
// 			return true, nil
// 		}

// 		return false, nil
// 	}
// }
