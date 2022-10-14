package prompt

import (
	"github.com/AlecAivazis/survey/v2"
)

func AskOne(msg string, options []string, description func(value string, index int) string) (answer string) {
	prompt := &survey.Select{
		Message: msg,
		Options: options,
		Description: description,
	}
	survey.AskOne(prompt, &answer)
	return
}
