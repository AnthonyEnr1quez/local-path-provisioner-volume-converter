package main

import (
	"log"

	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/kube"
	"github.com/AnthonyEnr1quez/local-path-provisioner-volume-converter/internal/prompt"
)

func main() {
	log.Print("Use \"Ctrl+C\" to quit\n\n")

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
		resourceNamespace, resourceName, volume, patcher, err := prompt.Survey(cw)
		if err != nil {
			log.Println(err.Error())
			if err == terminal.InterruptErr {
				break
			}
			continue
		}

		err = kube.ConvertVolume(cw, resourceNamespace, resourceName, volume, patcher)
		if err != nil {
			log.Fatalln(err.Error())
		}
	}
}
