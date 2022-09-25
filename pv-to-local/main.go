package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"context"

	clientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
func main() {
	// jobName := flag.String("jobname", "test-job", "The name of the job")
	// containerImage := flag.String("image", "ubuntu:latest", "Name of the container image")
	// entryCommand := flag.String("command", "ls", "The command to run inside the container")

	// flag.Parse()

	// fmt.Printf("Args : %s %s %s\n", *jobName, *containerImage, *entryCommand)
	fmt.Print("test")
	cs := connectToK8s()
	fmt.Print(cs)

	pvs, _ := cs.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	
	for _, pv := range pvs.Items {
		if pv.Spec.PersistentVolumeSource.HostPath != nil {
			fmt.Println(pv.Name)
		}
	}
	
	fmt.Print(pvs)
}

func connectToK8s() *kubernetes.Clientset {
	home, exists := os.LookupEnv("HOME")
	if !exists {
			home = "/root"
	}

	configPath := filepath.Join(home, ".config", "kube", "config")

	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
			log.Panicln("failed to create K8s config")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
			log.Panicln("Failed to create K8s clientset")
	}

	return clientset
}
