package bot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Registration struct {
	DisplayName   string
	NameRequested bool
}

type RegistrationResult struct {
	Handled     bool
	DisplayName string
	Reply       string
}

func Register(state Registration, text string) RegistrationResult {
	if state.DisplayName != "" {
		return RegistrationResult{}
	}
	question := "Как вас зовут? Пришлите имя одним сообщением (до 80 символов)."
	if !state.NameRequested || strings.HasPrefix(strings.TrimSpace(text), "/start") {
		return RegistrationResult{Handled: true, Reply: question}
	}
	name := strings.TrimSpace(text)
	if !validName(name) {
		return RegistrationResult{Handled: true, Reply: "Не удалось распознать имя. " + question}
	}
	return RegistrationResult{Handled: true, DisplayName: name, Reply: "Приятно познакомиться, " + name + "! Теперь пришлите расход, например «кофе 350», или попросите показать расходы за неделю."}
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
