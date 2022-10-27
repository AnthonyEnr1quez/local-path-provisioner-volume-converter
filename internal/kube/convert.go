package kube

import (
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func ConvertVolume(cw ClientWrapper, resourceType, resourceNamespace, resourceName string, volume *corev1.PersistentVolume) {
	// TODO
	pvcName := volume.Spec.ClaimRef.Name
	volumeName := pvcName[strings.IndexByte(pvcName, '-')+1:]
	pvcNamespace := volume.Spec.ClaimRef.Namespace
	volumeSize := volume.Spec.Capacity.Storage().String()

	fmt.Printf("\nConverting PVC %s from host path volume to local volume\n\n", pvcName)

	patchy, err := NewPatcher(resourceType)
	if err != nil {
		log.Fatalln(err)
	}

	tempPVCName, err := cw.AddTempPVC(patchy, resourceNamespace, resourceName, volumeName, volumeSize)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPVCBound(pvcNamespace, tempPVCName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, resourceName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.ScaleDeployment(pvcNamespace, resourceName, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	jobName, err := cw.MigrateJob(pvcNamespace, pvcName, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsJobFinished(migrationNamespace, jobName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UpdateOriginalPVC(patchy, resourceNamespace, resourceName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPVCBound(pvcNamespace, pvcName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, resourceName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.ScaleDeployment(pvcNamespace, resourceName, 0)
	if err != nil {
		log.Fatalln(err.Error())
	}

	jobName, err = cw.MigrateJob(pvcNamespace, tempPVCName, pvcName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsJobFinished(migrationNamespace, jobName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.UnbindTempPVC(patchy, resourceNamespace, resourceName, volumeName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = cw.DeletePVC(pvcNamespace, tempPVCName)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, resourceName))
	if err != nil {
		log.Fatalln(err.Error())
	}

	fmt.Printf("\nPVC %s converted\n\n", pvcName)

	fmt.Print("Make sure to add the following block to the PVC declaration of your resource definition file if used.\n\n")
	fmt.Print("annotations: \n  volumeType: local\n\n")
}
