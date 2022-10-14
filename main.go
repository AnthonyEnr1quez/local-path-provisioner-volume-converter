package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	_ "embed"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/samber/lo"
)

func main() {
	cw := kube.GetClientWrapper()

	namespaces, err := cw.GetNamespaces()
	if err != nil {
		log.Fatalln(err.Error())
	}

	helmChartsByNamespace := lo.Associate(namespaces, func(n corev1.Namespace) (string, []unstructured.Unstructured) {
		charts, err := cw.GetHelmCharts(n.Name)
		if err != nil {
			return "", nil
		}
		return n.Name, charts
	})

	nsNamesWithHCCount := lo.FilterMap(lo.Entries(helmChartsByNamespace), func(n lo.Entry[string, []unstructured.Unstructured], _ int) (string, bool) {
		if len(n.Value) != 0 {
			return fmt.Sprintf("%s: %d charts", n.Key, len(n.Value)), true
		}
		return "", false
	})

	var selectedNS1 string
	prompt := &survey.Select{
		Message: "Select namespace",
		Options: nsNamesWithHCCount,
	}
	survey.AskOne(prompt, &selectedNS1)

	selectedNS := selectedNS1[:strings.IndexByte(selectedNS1, ':')]

	selectedCharts := helmChartsByNamespace[selectedNS]

	volumesByHelmChartName := lo.Associate(selectedCharts, func(chart unstructured.Unstructured) (string, []corev1.Volume) {
		if chart.Object["spec"].(map[string]interface{})["targetNamespace"] != nil {
			podNS := chart.Object["spec"].(map[string]interface{})["targetNamespace"].(string)
			chartName := chart.Object["metadata"].(map[string]interface{})["name"].(string)
			pod, err := cw.GetPodByName(podNS, chartName)
			if err != nil {
				return "", nil
			}

			volumesToUpdate := lo.Filter(pod.Spec.Volumes, func(vol corev1.Volume, _ int) bool {
				if vol.PersistentVolumeClaim != nil {
					pvcName := vol.PersistentVolumeClaim.ClaimName
					pv, err := cw.GetPVFromPVCName(podNS, pvcName)
					if err != nil {
						return false
					}

					if pv.Spec.PersistentVolumeSource.HostPath != nil {
						return true
					}
				}
				return false
			})

			return chartName, volumesToUpdate
		}
		return "", nil
	})

	helmChartNameWithVolCount := lo.FilterMap(lo.Entries(volumesByHelmChartName), func(n lo.Entry[string, []corev1.Volume], _ int) (string, bool) {
		if len(n.Value) != 0 {
			return fmt.Sprintf("%s: %d volumes", n.Key, len(n.Value)), true
		}
		return "", false
	})

	var selectedChart1 string
	prompt = &survey.Select{
		Message: "Select Chart",
		Options: helmChartNameWithVolCount,
	}
	survey.AskOne(prompt, &selectedChart1)
	selectedChart := selectedChart1[:strings.IndexByte(selectedChart1, ':')]

	for _, vol := range volumesByHelmChartName[selectedChart] {
		pvcName := vol.PersistentVolumeClaim.ClaimName
		podNS := "test"

		fmt.Println("Doing stuff to this pvc:", pvcName)
		// writeExtraFile(cs, config, podNS, sonarrPod.Name)

		tempPVCName, err := cw.AddTempPVC(selectedNS, selectedChart, vol.Name)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.WaitFor(cw.IsPVCBound(podNS, tempPVCName))
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.WaitFor(cw.IsPodReady(podNS, selectedChart))
		if err != nil {
			log.Fatalln(err.Error())
		}
		fmt.Println("pod ready after first bind")

		err = cw.ScaleDeployment(podNS, selectedChart, 0)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.PvMigrater(podNS, pvcName, tempPVCName)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = cw.DeletePVC(podNS, pvcName)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = cw.UpdateOriginalPVC(selectedNS, selectedChart, vol.Name)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.WaitFor(cw.IsPVCBound(podNS, tempPVCName))
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.WaitFor(cw.IsPodReady(podNS, selectedChart))
		if err != nil {
			log.Fatalln(err.Error())
		}
		fmt.Println("pod ready after second bind")

		err = cw.ScaleDeployment(podNS, selectedChart, 0)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.PvMigrater(podNS, tempPVCName, pvcName)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = cw.UnbindTempPVC(selectedNS, selectedChart, vol.Name)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = cw.DeletePVC(podNS, tempPVCName)
		if err != nil {
			log.Fatalln(err.Error())
		}

		err = kube.WaitFor(cw.IsPodReady(podNS, selectedChart))
		if err != nil {
			log.Fatalln(err.Error())
		}
		fmt.Println("pod ready after third bind")

		fmt.Print("asdf")
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
