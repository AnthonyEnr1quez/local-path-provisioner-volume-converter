package kube

import (
	"fmt"
	"log"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func WaitFor(condition wait.ConditionFunc) error {
	return wait.PollImmediateInfinite(time.Second, condition)
}

func (cw *ClientWrapper) IsPVCBound(namespace, pvcName string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Print(".")

		pvc, err := cw.GetPVCByName(namespace, pvcName)
		if err != nil {
			return false, nil
		}

		switch pvc.Status.Phase {
		case corev1.ClaimBound:
			pv, _ := cw.GetPVByName(pvc.Spec.VolumeName)
			// TODO possibly recreate volume?
			if pv.Spec.PersistentVolumeSource.Local == nil {
				return false, nil
			}
			log.Printf("\nNew PVC %s bound\n", pvcName)
			return true, nil
		default:
			return false, nil
		}
	}
}

func (cw *ClientWrapper) IsPodReady(namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Print(".")

		pod, err := cw.GetPodByName(namespace, name)
		if err != nil {
			return false, nil
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == "True" {
				log.Printf("\n%s pod bound\n", pod.Name)
				return true, nil
			}
		}

		return false, nil
	}
}

func (cw *ClientWrapper) IsJobFinished(namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Print(".")

		job, err := cw.getJobByName(namespace, name)
		if err != nil {
			return false, nil
		}

		for _, cond := range job.Status.Conditions {
			if cond.Type == batchv1.JobComplete && cond.Status == "True" {
				log.Printf("%s job complete\n", name)
				return true, nil
			}
		}

		return false, nil
	}
}

func (cw *ClientWrapper) isPodScaled(namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Print(".")

		_, err := cw.GetPodByName(namespace, name)
		if err != nil && strings.Contains(err.Error(), "not ready yet") {
			return true, nil
		}

		return false, nil
	}
}
