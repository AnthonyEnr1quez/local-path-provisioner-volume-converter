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

const (
	tmpDirName = "local-path-provisioner-volume-converter"
	tmpBinName = "pv-migrate"
)

//go:embed bin/pv-migrate
var pvMigrateBin []byte

func getCachePath() (string, error) {
	userCachePath, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	cachePath := fmt.Sprintf("%s/%s", userCachePath, tmpDirName)
	return cachePath, nil
}

func CreateTempFiles() error {
	CleanUpTempFiles()
	cachePath, err := getCachePath()
	if err != nil {
		return err
	}

	err = os.Mkdir(cachePath, os.ModePerm)
	if err != nil {
		return err
	}

	return os.WriteFile(fmt.Sprintf("%s/%s", cachePath, tmpBinName), pvMigrateBin, 0755)
}

func CleanUpTempFiles() error {
	cachePath, err := getCachePath()
	if err != nil {
		return err
	}

	return os.RemoveAll(cachePath)
}

// TODO need -d on second write? https://github.com/utkuozdemir/pv-migrate/blob/master/USAGE.md
func PvMigrater(namespace, fromPVC, toPVC string) error {
	cachePath, err := getCachePath()
	if err != nil {
		return err
	}

	cmd := exec.Command(fmt.Sprintf("%s/%s", cachePath, tmpBinName), "migrate", fromPVC, toPVC, "-n", namespace, "-N", namespace)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError
		}
	}

	if !strings.Contains(out.String(), "Migration succeeded") {
		fmt.Println("\n"+out.String())
		return errors.New("pv migration failed")
	}

	fmt.Println("\n"+out.String())
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
