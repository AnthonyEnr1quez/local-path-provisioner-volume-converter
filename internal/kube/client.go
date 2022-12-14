package kube

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	HelmChartResource = schema.GroupVersionResource{
		Group:    "helm.cattle.io",
		Version:  "v1",
		Resource: "helmcharts",
	}
	FluxHelmReleaseResource = schema.GroupVersionResource{
		Group:    "helm.toolkit.fluxcd.io",
		Version:  "v2beta1",
		Resource: "helmreleases",
	}
)

type ClientWrapper struct {
	dc dynamic.Interface
	cs kubernetes.Interface
}

func GetClientWrapper(config *rest.Config) ClientWrapper {
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

func GetKubeconfig() (*rest.Config, error) {
	configPath, found := os.LookupEnv("KUBECONFIG")
	if !found {
		return nil, errors.New("KUBECONFIG env var does not exist")
	}

	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return nil, errors.New("failed to create K8s config")
	}

	return config, nil
}

func (cw *ClientWrapper) GetNamespaces() ([]corev1.Namespace, error) {
	namespaces, err := cw.cs.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return namespaces.Items, nil
}

func (cw *ClientWrapper) GetResourceList(namespace string, resource schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	resources, err := cw.dc.Resource(resource).Namespace(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return resources.Items, nil
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

func (cw *ClientWrapper) GetPVByName(name string) (*corev1.PersistentVolume, error) {
	return cw.cs.CoreV1().PersistentVolumes().Get(context.Background(), name, metav1.GetOptions{})
}

func (cw *ClientWrapper) GetPVCByName(namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	return cw.cs.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func (cw *ClientWrapper) GetPVCsByResourceName(namespace, name string) ([]corev1.PersistentVolumeClaim, error) {
	pvcs, err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", name)})
	return pvcs.Items, err
}

func (cw *ClientWrapper) getJobByName(namespace, name string) (*batchv1.Job, error) {
	return cw.cs.BatchV1().Jobs(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func (cw *ClientWrapper) CreateNamespace(name string) error {
	_, err := cw.cs.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
	return err
}

func (cw *ClientWrapper) DeleteNamespace(name string) error {
	return cw.cs.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{})
}

func (cw *ClientWrapper) CreateServiceAccount(namespace, name string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	_, err := cw.cs.CoreV1().ServiceAccounts(namespace).Create(context.Background(), sa, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "edit",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: namespace,
				Name:      name,
			},
		},
	}
	_, err = cw.cs.RbacV1().ClusterRoleBindings().Create(context.Background(), crb, metav1.CreateOptions{})

	return err
}

func (cw *ClientWrapper) DeleteCRB(name string) error {
	return cw.cs.RbacV1().ClusterRoleBindings().Delete(context.Background(), name, metav1.DeleteOptions{})
}

func (cw *ClientWrapper) DeletePVC(namespace, name string) error {
	deletePolicy := metav1.DeletePropagationForeground
	err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})

	if err == nil {
		log.Println("PVC", name, "deleted")
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

	log.Printf("%s deployment finished scaling\n", name)
	return nil
}

func (cw *ClientWrapper) CreateJob(namespace string, job *batchv1.Job) (string, error) {
	job, err := cw.cs.BatchV1().Jobs(namespace).Create(context.Background(), job, metav1.CreateOptions{})
	if err != nil {
		return "", nil
	}
	return job.Name, nil
}
