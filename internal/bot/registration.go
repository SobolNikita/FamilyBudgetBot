package bot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Registration struct {
	DisplayName   string
	NameRequested bool
	PendingName   string
}

type RegistrationResult struct {
	Handled     bool
	DisplayName string
	Reply       string
	PendingName string
}

func Register(state Registration, text string) RegistrationResult {
	if strings.TrimSpace(text) == "/name" {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, Reply: "Как вас зовут? Пришлите новое имя одним сообщением (до 80 символов).", PendingName: "?"}
	}
	if state.DisplayName != "" && state.PendingName == "" {
		return RegistrationResult{}
	}
	question := "Как вас зовут? Пришлите имя одним сообщением (до 80 символов)."
	if state.PendingName != "" && state.PendingName != "?" {
		switch Confirmation(text) {
		case "confirm":
			return RegistrationResult{Handled: true, DisplayName: state.PendingName, Reply: "Имя сохранено: " + state.PendingName + ". Теперь пришлите расход."}
		case "cancel":
			return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: "?", Reply: question}
		}
	}
	if !state.NameRequested || strings.HasPrefix(strings.TrimSpace(text), "/start") {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: "?", Reply: question}
	}
	name := strings.TrimSpace(text)
	if !validName(name) {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: "?", Reply: "Не удалось распознать имя. " + question}
	}
	return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: name, Reply: "Сохранить имя «" + name + "»? Ответьте «да» или «нет». Расходы из сообщений знакомства не сохраняются; после подтверждения пришлите их заново."}
}

func validName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.HasPrefix(name, "/") || name == "[неподдерживаемый тип сообщения]" {
		return false
	}
	hasLetter := false
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
		if unicode.IsLetter(character) {
			hasLetter = true
		}
	}
	return hasLetter
}
