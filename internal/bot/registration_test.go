package bot

import (
	"strings"
	"testing"
)

func TestRegistrationAsksBeforeAcceptingName(t *testing.T) {
	for _, text := range []string{"/start", "Привет", "кофе 350", "Анна"} {
		result := Register(Registration{}, text)
		if !result.Handled || result.DisplayName != "" || !strings.Contains(result.Reply, "Как вас зовут?") {
			t.Fatalf("first message treated as a name: %+v", result)
		}
	}
	result := Register(Registration{NameRequested: true}, "  Анна-Мария  ")
	if !result.Handled || result.DisplayName != "Анна-Мария" {
		t.Fatalf("name not normalized: %+v", result)
	}
}

func TestRegistrationRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", " ", "/start", "/start invite", "/help", "12345", "[неподдерживаемый тип сообщения]", "Анна\nМария", strings.Repeat("я", 81)} {
		result := Register(Registration{NameRequested: true}, name)
		if !result.Handled || result.DisplayName != "" {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
}

func TestRegisteredUserSkipsOnboarding(t *testing.T) {
	for _, text := range []string{"/start", "кофе 350", "Другое имя"} {
		if result := Register(Registration{DisplayName: "Анна"}, text); result.Handled {
			t.Fatal("existing participant registered again")
		}
	}
}
