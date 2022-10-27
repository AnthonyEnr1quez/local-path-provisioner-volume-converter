package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func TestConversion(t *testing.T) {
	ctx := context.Background()
	requests := testcontainers.ParallelContainerRequest{
		{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "rancher/k3s:latest",
				ExposedPorts: []string{"6443/tcp", "8443/tcp"},
				Privileged:   true,
				Cmd:          []string{"server"},
				WaitingFor:   wait.ForLog("Node controller sync successful"),
				Name:         "go-k3s",
			},
			Started: true,
		},
		{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:      "fluxcd/flux-cli:v0.36.0",
				Entrypoint: []string{"tail", "-f", "/dev/null"},
				Name:       "go-flux",
			},
			Started: true,
		},
	}

	res, err := testcontainers.ParallelContainers(ctx, requests, testcontainers.ParallelContainersOptions{WorkersCount: 2})
	require.NoError(t, err)

	for _, c := range res {
		defer c.Terminate(ctx)
	}

	var k3sContainer, fluxContainer testcontainers.Container
	for _, c := range res {
		name, _ := c.Name(ctx)
		if strings.Contains(name, "k3s") {
			k3sContainer = c
		} else if strings.Contains(name, "flux") {
			fluxContainer = c
		}
	}

	configBytes, err := getRawKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)

	err = installFlux(ctx, configBytes, k3sContainer, fluxContainer)
	require.NoError(t, err)

	restConfig, err := getRestConfig(ctx, configBytes, k3sContainer)
	require.NoError(t, err)

	// todo
	cs, err := kubernetes.NewForConfig(restConfig)
	dc, err := dynamic.NewForConfig(restConfig)

	err = updateProvisionerImage(cs)

	// charts repo
	_, err = createResourceFromFile(dc, "helmrepositories", "yaml/flux/charts.yaml")
	// test ns
	cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, metav1.CreateOptions{})

	cw := kube.GetClientWrapper(restConfig)
	
	err = cw.CreateMigrationNamespaceAndServiceAccount()
	require.NoError(t, err)
	defer cw.CleanupMigrationObjects()

	tests := []struct {
		resourceType      string
		resourceLocation  string
		pvcName           string
		selectedNamespace string
		selectedChartName string
		volumeName        string
		pvcNamespace      string
	}{
		{
			resourceType:      "helmreleases",
			resourceLocation:  "yaml/flux/helm-release.yaml",
			pvcName:           "sonarr-config",
			selectedNamespace: "test",
			selectedChartName: "sonarr",
			volumeName:        "config",
			pvcNamespace:      "test",
		},
		{
			resourceType:      "helmcharts",
			resourceLocation:  "yaml/radarr.yaml",
			pvcName:           "radarr-config",
			selectedNamespace: "kube-system",
			selectedChartName: "radarr",
			volumeName:        "config",
			pvcNamespace:      "test",
		},
	}
	for _, test := range tests {
		// test := test
		t.Run(test.resourceType, func(t *testing.T) {
			// t.Parallel()
			selectedChart, err := createResourceFromFile(dc, test.resourceType, test.resourceLocation)
			require.NoError(t, err)
			kube.ConvertVolume(cw, selectedChart, test.pvcName, test.selectedNamespace, test.selectedChartName, test.volumeName, test.pvcNamespace, "1Gi")
		})
	}
}

func getRawKubeConfig(ctx context.Context, c testcontainers.Container) ([]byte, error) {
	reader, err := c.CopyFileFromContainer(ctx, "/etc/rancher/k3s/k3s.yaml")
	if err != nil {
		return nil, err
	}

	return io.ReadAll(reader)
}

func getRestConfig(ctx context.Context, config []byte, k3sC testcontainers.Container) (*rest.Config, error) {
	port, err := k3sC.MappedPort(ctx, "6443/tcp")
	if err != nil {
		return nil, err
	}

	host, err := k3sC.Host(ctx)
	if err != nil {
		return nil, err
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
	if err != nil {
		return nil, err
	}

	restConfig.Host = fmt.Sprintf("https://%s:%s/", host, port.Port())

	return restConfig, nil
}

func installFlux(ctx context.Context, config []byte, k3sC, fluxC testcontainers.Container) error {
	k3sIP, err := k3sC.ContainerIP(ctx)
	if err != nil {
		return err
	}

	patchedConfig := bytes.ReplaceAll(config, []byte("127.0.0.1"), []byte(k3sIP))

	err = fluxC.CopyToContainer(ctx, patchedConfig, "/kubeconfig", 700)
	if err != nil {
		return err
	}

	exitCode, output, err := fluxC.Exec(ctx, []string{"flux", "install", "--kubeconfig=kubeconfig"})
	if err != nil {
		return err
	} else if exitCode != 0 {
		out, err := io.ReadAll(output)
		if err != nil {
			return err
		}
		return errors.New(fmt.Sprintf("flux install failed: %s", out))
	}
	return nil
}

func updateProvisionerImage(cs kubernetes.Interface) (err error) {
	deployments := cs.AppsV1().Deployments("kube-system")
	lpp, err := deployments.Get(context.Background(), "local-path-provisioner", metav1.GetOptions{})
	if err != nil {
		return
	}

	lpp.Spec.Template.Spec.Containers[0].Image = "rancher/local-path-provisioner:v0.0.23"
	_, err = deployments.Update(context.TODO(), lpp, metav1.UpdateOptions{})

	return
}

func createResourceFromFile(dc dynamic.Interface, resourceType, fileName string) (unstructured.Unstructured, error) {
	fileBytes, err := os.ReadFile(fileName)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	obj := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, gvk, err := dec.Decode(fileBytes, nil, obj)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	namespace, found, err := unstructured.NestedString(obj.Object, "metadata", "namespace")
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	if !found {
		return unstructured.Unstructured{}, errors.New("namespace not found")
	}

	gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resourceType}
	out, err := dc.Resource(gvr).Namespace(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})

	return *out, err
}
