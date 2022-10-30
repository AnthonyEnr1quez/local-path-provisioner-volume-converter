package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
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

	cw := GetClientWrapper(restConfig)

	err = updateProvisionerImage(cw.cs)
	require.NoError(t, err)

	_, _, err = createResourceFromFile(cw.dc, "helmrepositories", "test_data/helm-repository.yaml")
	require.NoError(t, err)

	err = cw.CreateMigrationNamespaceAndServiceAccount()
	require.NoError(t, err)
	defer cw.CleanupMigrationObjects()

	tests := []struct {
		resourceName      string
		pvcNamespace      string
		patcher           Patcher
	}{
		{
			resourceName:      "helm-release",
			pvcNamespace:      "default",
			patcher:           HelmReleasePatcher{},
		},
		{
			resourceName:      "helm-chart",
			pvcNamespace:      "default",
			patcher:           HelmChartPatcher{},
		},
	}
	for _, test := range tests {
		// test := test
		resourceType := test.patcher.getResource().Resource
		t.Run(resourceType, func(t *testing.T) {
			// t.Parallel()

			file := "/config/hello.txt"
			fileContents := "Hello World!"

			_, resourceNamespace, err := createResourceFromFile(cw.dc, resourceType, fmt.Sprintf("test_data/%s.yaml", test.resourceName))
			require.NoError(t, err)

			err = WaitFor(cw.IsPodReady(test.pvcNamespace, test.resourceName))
			if err != nil {
				log.Fatalln(err.Error())
			}

			pod, err := cw.GetPodByName(test.pvcNamespace, test.resourceName)
			require.NoError(t, err)

			_, es, err := execInPod(cw.cs, restConfig, &pod, fmt.Sprintf("echo \"%s\" > %s", fileContents, file))
			require.NoError(t, err)
			require.Empty(t, es)

			pvc, err := cw.GetPVCByName(test.pvcNamespace, fmt.Sprintf("%s-config", test.resourceName))
			require.NoError(t, err)

			volume, err := cw.GetPVByName(pvc.Spec.VolumeName)
			require.NoError(t, err)

			err = ConvertVolume(cw, resourceNamespace, test.resourceName, volume, test.patcher)
			require.NoError(t, err)

			pod, err = cw.GetPodByName(test.pvcNamespace, test.resourceName)
			require.NoError(t, err)

			output, es, err := execInPod(cw.cs, restConfig, &pod, fmt.Sprintf("cat %s", file))
			require.NoError(t, err)
			require.Empty(t, es)

			assert.Contains(t, output, fileContents)
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

func createResourceFromFile(dc dynamic.Interface, resourceType, fileName string) (unstructured.Unstructured, string, error) {
	fileBytes, err := os.ReadFile(fileName)
	if err != nil {
		return unstructured.Unstructured{}, "", err
	}

	obj := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, gvk, err := dec.Decode(fileBytes, nil, obj)
	if err != nil {
		return unstructured.Unstructured{}, "", err
	}

	namespace, found, err := unstructured.NestedString(obj.Object, "metadata", "namespace")
	if err != nil {
		return unstructured.Unstructured{}, "", err
	}
	if !found {
		return unstructured.Unstructured{}, "", errors.New("namespace not found")
	}

	gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resourceType}
	out, err := dc.Resource(gvr).Namespace(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})

	return *out, namespace, err
}

func execInPod(cs kubernetes.Interface, config *rest.Config, pod *corev1.Pod, command string) (string, string, error) {
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}

	request := cs.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"/bin/sh", "-c", command},
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", request.URL())
	if err != nil {
		return "", "", err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: buf,
		Stderr: errBuf,
	})
	if err != nil {
		return "", "", err
	}

	return buf.String(), errBuf.String(), nil
}
