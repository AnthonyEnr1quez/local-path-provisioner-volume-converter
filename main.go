package main

import (
	"fmt"
	"log"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/samber/lo"
)

/// TODO just throw it all in while loop until they quit

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

	// nsNamesWithHCCount := lo.FilterMap(lo.Entries(helmChartsByNamespace), func(n lo.Entry[string, []unstructured.Unstructured], _ int) (string, bool) {
	// 	if len(n.Value) != 0 {
	// 		return fmt.Sprintf("%s: %d charts", n.Key, len(n.Value)), true
	// 	}
	// 	return "", false
	// })

	var selectedNS string
	prompt := &survey.Select{
		Message: "Select namespace",
		Options: lo.Keys(helmChartsByNamespace),
		Description: func(value string, index int) string {
			return strconv.Itoa(len(helmChartsByNamespace[value]))
		},
	}
	survey.AskOne(prompt, &selectedNS)

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

	// helmChartNameWithVolCount := lo.FilterMap(lo.Entries(volumesByHelmChartName), func(n lo.Entry[string, []corev1.Volume], _ int) (string, bool) {
	// 	if len(n.Value) != 0 {
	// 		return fmt.Sprintf("%s: %d volumes", n.Key, len(n.Value)), true
	// 	}
	// 	return "", false
	// })

	var selectedChart string
	prompt = &survey.Select{
		Message: "Select Chart",
		Options: lo.Keys(volumesByHelmChartName),
		Description: func(value string, index int) string {
			return strconv.Itoa(len(volumesByHelmChartName[value]))
		},
	}
	survey.AskOne(prompt, &selectedChart)

	volNames := lo.Map(volumesByHelmChartName[selectedChart], func(vol corev1.Volume, _ int) string {
		return vol.PersistentVolumeClaim.ClaimName
	})

	var selectedVolumeName string
	prompt = &survey.Select{
		Message: "Select Volume",
		Options: volNames,
	}
	survey.AskOne(prompt, &selectedVolumeName)

	vol, _ := lo.Find(volumesByHelmChartName[selectedChart], func(vol corev1.Volume) bool {
		return vol.PersistentVolumeClaim.ClaimName == selectedVolumeName
	})

	pvcName := vol.PersistentVolumeClaim.ClaimName
	// TODO, can read from chart
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

	fmt.Println("DONE")
}
