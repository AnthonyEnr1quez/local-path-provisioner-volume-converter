package kube

import (
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func ConvertVolume(cw ClientWrapper, resourceNamespace, resourceName string, volume *corev1.PersistentVolume, patcher Patcher) (err error) {
	pvcName := volume.Spec.ClaimRef.Name
	volumeName := pvcName[strings.LastIndexByte(pvcName, '-')+1:]
	pvcNamespace := volume.Spec.ClaimRef.Namespace
	volumeSize := volume.Spec.Capacity.Storage().String()

	log.Printf("\nConverting PVC %s from host path volume to local volume\n\n", pvcName)

	tempPVCName, err := cw.AddTempPVC(patcher, resourceNamespace, resourceName, volumeName, volumeSize)
	if err != nil {
		return
	}

	err = WaitFor(cw.IsPVCBound(pvcNamespace, tempPVCName))
	if err != nil {
		return
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, resourceName))
	if err != nil {
		return
	}

	err = cw.ScaleDeployment(pvcNamespace, resourceName, 0)
	if err != nil {
		return
	}

	jobName, err := cw.MigrateJob(pvcNamespace, pvcName, tempPVCName)
	if err != nil {
		return
	}

	err = WaitFor(cw.IsJobFinished(migrationNamespace, jobName))
	if err != nil {
		return
	}

	err = cw.DeletePVC(pvcNamespace, pvcName)
	if err != nil {
		return
	}

	err = cw.UpdateOriginalPVC(patcher, resourceNamespace, resourceName, volumeName)
	if err != nil {
		return
	}

	err = WaitFor(cw.IsPVCBound(pvcNamespace, pvcName))
	if err != nil {
		return
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, resourceName))
	if err != nil {
		return
	}

	err = cw.ScaleDeployment(pvcNamespace, resourceName, 0)
	if err != nil {
		return
	}

	jobName, err = cw.MigrateJob(pvcNamespace, tempPVCName, pvcName)
	if err != nil {
		return
	}

	err = WaitFor(cw.IsJobFinished(migrationNamespace, jobName))
	if err != nil {
		return
	}

	err = cw.UnbindTempPVC(patcher, resourceNamespace, resourceName, volumeName)
	if err != nil {
		return
	}

	err = cw.DeletePVC(pvcNamespace, tempPVCName)
	if err != nil {
		return
	}

	err = WaitFor(cw.IsPodReady(pvcNamespace, resourceName))
	if err != nil {
		return
	}

	log.Printf("PVC %s converted\n\n", pvcName)

	fmt.Print("Make sure to add the following block to the PVC declaration of your resource definition file if used.\n\n")
	fmt.Print("annotations: \n  volumeType: local\n\n")

	return
}
