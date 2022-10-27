package main

import (
	"fmt"
	"log"

	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/prompt"
)

func main() {
	fmt.Print("Use \"Ctrl+C\" to quit\n\n")

	config, err := kube.GetKubeconfig()
	if err != nil {
		log.Fatalln(err.Error())
	}

	cw := kube.GetClientWrapper(config)

	err = cw.CreateMigrationNamespaceAndServiceAccount()
	if err != nil {
		log.Fatalln(err.Error())
	}
	defer cw.CleanupMigrationObjects()

	for {
		resourceType, resourceNamespace, resourceName, volume, err := prompt.Survey(cw)
		if err != nil {
			log.Println(err.Error())
			if err == terminal.InterruptErr {
				break
			}
			continue
		}

		err = kube.ConvertVolume(cw, resourceType, resourceNamespace, resourceName, volume)
		if err != nil {
			log.Fatalln(err.Error())
		}
	}
}
