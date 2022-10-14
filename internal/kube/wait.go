package kube

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func WaitFor(condition wait.ConditionFunc) error {
	return wait.PollImmediateInfinite(time.Second, condition)
}

func (cw *ClientWrapper) IsPVCBound(namespace, pvcName string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Printf(".") // progress bar!

		pvc, err := cw.cs.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), pvcName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		switch pvc.Status.Phase {
		case corev1.ClaimBound:
			pv, _ := cw.GetPVFromPVCName(namespace, pvcName)
			if pv.Spec.PersistentVolumeSource.Local == nil {
				// TODO handle
				fmt.Printf("%s is not a local volume", pvcName)
			}
			fmt.Println("new pvc bound")
			return true, nil
		default:
			return false, nil
		}
	}
}

func (cw *ClientWrapper) IsPodReady(namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Printf(".") // progress bar!

		pod, err := cw.GetPodByName(namespace, name)
		if err != nil {
			fmt.Println(err.Error())
			return false, nil
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == "True" {
				return true, nil
			}
		}
		return false, nil
	}
}

func (cw *ClientWrapper) isPodScaled(namespace, name string) wait.ConditionFunc {
	return func() (bool, error) {
		fmt.Printf(".") // progress bar!

		_, err := cw.GetPodByName(namespace, name)
		if err != nil && strings.Contains(err.Error(), "not ready yet") {
			fmt.Println("scaled")
			return true, nil
		}

		return false, nil
	}
}