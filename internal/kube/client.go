package kube

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var helmChartResource = schema.GroupVersionResource{
	Group:    "helm.cattle.io",
	Version:  "v1",
	Resource: "helmcharts",
}

type ClientWrapper struct {
	dc dynamic.Interface
	cs kubernetes.Interface
}

func GetClientWrapper() ClientWrapper {
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

	return ClientWrapper{
		dc: dc,
		cs: cs,
	}
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

// GETTERS

func (cw *ClientWrapper) GetNamespaces() ([]corev1.Namespace, error) {
	namespaces, err := cw.cs.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return namespaces.Items, nil
}

func (cw *ClientWrapper) GetHelmCharts(namespace string) ([]unstructured.Unstructured, error) {
	helmCharts, err := cw.dc.Resource(helmChartResource).Namespace(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return helmCharts.Items, nil
}

func (cw *ClientWrapper) GetPodByName(namespace, name string) (corev1.Pod, error) {
	pods, err := cw.cs.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", name)})
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

func (cw *ClientWrapper) GetPVFromPVC(pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolume, error) {
	return cw.cs.CoreV1().PersistentVolumes().Get(context.Background(), pvc.Spec.VolumeName, metav1.GetOptions{})
}

func (cw *ClientWrapper) GetPVCsByChartName(namespace, name string) ([]corev1.PersistentVolumeClaim, error) {
	pvcs, err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", name)})
	return pvcs.Items, err
}

// SETTERS

func (cw *ClientWrapper) DeletePVC(namespace, name string) error {
	deletePolicy := metav1.DeletePropagationForeground
	err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})

	if err == nil {
		fmt.Println("PVC", name, "deleted")
	}
	return err
}

func (cw *ClientWrapper) ScaleDeployment(namespace, name string, replicas int) error {
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

	err = WaitFor(cw.isPodScaled(namespace, name))
	if err != nil {
		return err
	}

	fmt.Println("scaled to 0")
	return nil
}

// DA BIN

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

// 		pod, err := GetPodByName(cs, namespace, name)
// 		if err != nil {
// 			fmt.Println(err.Error())
// 			return false, nil
// 		}

// 		switch pod.Status.Phase {
// 		case corev1.PodRunning:
// 			fmt.Println("running")
// 			return true, nil
// 		case corev1.PodFailed, corev1.PodSucceeded:
// 			return false, nil
// 		}
// 		return false, nil
// 	}
// }

// func isPodDeleted(cs kubernetes.Interface, namespace, name string) wait.ConditionFunc {
// 	return func() (bool, error) {
// 		fmt.Printf(".") // progress bar!

// 		_, err := GetPodByName(cs, namespace, name)
// 		if err != nil && err.Error() == fmt.Sprintf("pods \"%s\" not found", name) {
// 			fmt.Println("deleted")
// 			return true, nil
// 		}

// 		return false, nil
// 	}
// }
