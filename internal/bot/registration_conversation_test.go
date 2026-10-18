package bot

import (
	"strings"
	"testing"
)

func TestNameChangeUsesConversation(t *testing.T) {
	user := Registration{DisplayName: "Анна", NameRequested: true}
	for _, text := range []string{"изменить имя", "Хочу поменять имя", "смени моё имя", "пожалуйста, измените имя"} {
		result := Register(user, text)
		if !result.Handled || result.PendingName != "?" || result.DisplayName != user.DisplayName {
			t.Fatalf("rename intent missed: %q %+v", text, result)
		}
	}
	command := Register(user, "/name")
	if !command.Handled || command.PendingName != "" || command.DisplayName != user.DisplayName {
		t.Fatal("slash command started rename")
	}
	for _, text := range []string{"не хочу менять имя", "кофе 5", "поменяй имя у жены"} {
		if result := Register(user, text); result.Handled {
			t.Fatalf("unrelated text started rename: %q", text)
		}
	}
}

func TestNameConfirmationDoesNotSwallowExpensesOrDeleteConfirmation(t *testing.T) {
	user := Registration{DisplayName: "Анна", NameRequested: true, PendingName: "Аня"}
	for _, text := range []string{"кофе 5", "удали", "да, удали"} {
		result := Register(user, text)
		if result.DisplayName != "Анна" || result.PendingName != "Аня" {
			t.Fatalf("unexpected confirmation %q: %+v", text, result)
		}
	}
	cancel := Register(user, "отмена")
	if cancel.DisplayName != "Анна" || cancel.PendingName != "" {
		t.Fatal("rename not canceled")
	}
	confirm := Register(user, "да")
	if confirm.DisplayName != "Аня" || confirm.PendingName != "" {
		t.Fatal("rename not confirmed")
	}
	if strings.Contains(confirm.Reply, "знакомства") {
		t.Fatal("onboarding copy leaked into rename")
	}
}
