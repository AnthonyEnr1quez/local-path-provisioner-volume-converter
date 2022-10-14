package kube

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var (
	//go:embed bin/pv-migrate
	pvm []byte
)

func PvMigrater(namespace, fromPVC, toPVC string) error {
	// TODO https://pkg.go.dev/os#UserCacheDir
	// Only do once per exec 
	os.WriteFile("pv-migrate-bin-v1", pvm, 0755)
	defer os.Remove("pv-migrate-bin-v1")

	cmd := exec.Command("./pv-migrate-bin-v1", "migrate", fromPVC, toPVC, "-n", namespace, "-N", namespace)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError
		}
	}

	if !strings.Contains(out.String(), "Migration succeeded") {
		fmt.Println(out.String())
		return errors.New("pv migration failed")
	}

	fmt.Println(out.String())
	return nil
}

// TODO
// func writeExtraFile(cs kubernetes.Interface, config *rest.Config, namespace, podname string) {
// 	cmd := []string{
// 		"/bin/sh",
// 		"-c",
// 		"echo \"Hello World!\" > /config/hello.txt",
// 	}

// 	req := cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(podname).SubResource("exec")
// 	option := &corev1.PodExecOptions{
// 		Command: cmd,
// 		Stdin:   true,
// 		Stdout:  true,
// 		Stderr:  true,
// 		TTY:     true,
// 	}

// 	req.VersionedParams(
// 		option,
// 		scheme.ParameterCodec,
// 	)
// 	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	r, _ := io.Pipe()
// 	err = exec.Stream(remotecommand.StreamOptions{
// 		Stdin:  r,
// 		Stdout: os.Stdout,
// 		Stderr: os.Stderr,
// 		Tty:    false,
// 	})
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	fmt.Println("Files Written")
// }
