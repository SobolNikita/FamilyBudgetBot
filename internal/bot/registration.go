package bot

import (
	"regexp"
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
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "/name") {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: state.PendingName, Reply: "Чтобы поменять имя, просто напишите «изменить имя»."}
	}
	if WantsNameChange(text) {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, Reply: "Как вас называть? Напишите новое имя.", PendingName: "?"}
	}
	if state.DisplayName != "" && state.PendingName == "" {
		return RegistrationResult{}
	}
	question := "Как вас зовут? Напишите имя, которое будут видеть ваши близкие."
	answer := strings.ToLower(strings.Trim(strings.TrimSpace(text), ".! "))
	if state.DisplayName != "" && (answer == "отмена" || answer == "отмени" || answer == "не надо") {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, Reply: "Хорошо, оставим имя «" + state.DisplayName + "»."}
	}
	if state.PendingName != "" && state.PendingName != "?" {
		switch answer {
		case "да", "верно", "подтверждаю", "все верно", "всё верно":
			reply := "Готово! Теперь ваше имя — «" + state.PendingName + "»."
			if state.DisplayName == "" {
				reply = "Приятно познакомиться, " + state.PendingName + "! Теперь можно записывать расходы. Например: «кофе 5» или «такси 12 вчера». Если вы уже присылали покупку, напишите её ещё раз — я пока только узнавал ваше имя."
			}
			return RegistrationResult{Handled: true, DisplayName: state.PendingName, Reply: reply}
		case "нет", "отмена", "отмени", "не надо":
			return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: "?", Reply: question}
		default:
			return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: state.PendingName, Reply: "Назвать вас «" + state.PendingName + "»? Напишите «да», если всё верно, или «нет», чтобы выбрать другое имя."}
		}
	}
	if !state.NameRequested || strings.HasPrefix(strings.TrimSpace(text), "/start") {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: "?", Reply: question}
	}
	name := strings.TrimSpace(text)
	if !validName(name) {
		return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: "?", Reply: "Напишите, пожалуйста, имя одним сообщением — до 80 символов, без переносов строк."}
	}
	return RegistrationResult{Handled: true, DisplayName: state.DisplayName, PendingName: name, Reply: "Назвать вас «" + name + "»? Ответьте «да» или «нет»."}
}

var nameChange = regexp.MustCompile(`^(?:(?:я )?хочу |давай |пожалуйста,? )?(?:изменить|измени|измените|поменять|поменяй|поменяйте|сменить|смени|смените) (?:мое |моё )?имя(?:,? пожалуйста)?[.!?]*$`)

func WantsNameChange(text string) bool {
	return nameChange.MatchString(strings.ToLower(strings.TrimSpace(text)))
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
