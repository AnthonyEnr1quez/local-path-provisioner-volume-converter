package kube

import (
	"fmt"
	"log"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func ConvertVolume(cw ClientWrapper, selectedChart unstructured.Unstructured, pvcName, selectedNamespace, selectedChartName, volumeName, pvcNamespace, volumeSize string) {
	fmt.Printf("\nConverting PVC %s from host path volume to local volume\n\n", pvcName)

	patchy, err := NewPatcher(selectedChart)
	if err != nil {
		log.Fatalln(err)
	}

	tempPVCName, err := cw.AddTempPVC(patchy, selectedNamespace, selectedChartName, volumeName, volumeSize)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPVCBound(pvcNamespace, tempPVCName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, selectedChartName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.ScaleDeployment(pvcNamespace, selectedChartName, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	jobName, err := cw.MigrateJob(pvcNamespace, pvcName, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsJobFinished(MigrationNamespace, jobName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UpdateOriginalPVC(patchy, selectedNamespace, selectedChartName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPVCBound(pvcNamespace, pvcName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, selectedChartName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.ScaleDeployment(pvcNamespace, selectedChartName, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	jobName, err = cw.MigrateJob(pvcNamespace, tempPVCName, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsJobFinished(MigrationNamespace, jobName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UnbindTempPVC(patchy, selectedNamespace, selectedChartName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, selectedChartName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	fmt.Printf("\nPVC %s converted\n\n", pvcName)

	fmt.Print("Make sure to add the following block to the PVC declaration of your resource definition file if used.\n\n")
	fmt.Print("annotations: \n  volumeType: local\n\n")
}
