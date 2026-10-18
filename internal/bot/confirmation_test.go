package bot

import (
	"context"
	"familyexpenses/internal/expenses"
	"strings"
	"testing"
)

func TestNaturalConfirmationAndPendingPurchase(t *testing.T) {
	for _, text := range []string{"да, конечно", "Да, удали его!", "подтверждаю удаление"} {
		if Confirmation(text) != "confirm" {
			t.Fatalf("not confirmed: %s", text)
		}
	}
	for _, text := range []string{"не удаляй", "нет, не удаляй", "передумала"} {
		if Confirmation(text) != "cancel" {
			t.Fatalf("not canceled: %s", text)
		}
	}
	for _, text := range []string{"да, но не удаляй", "кофе 350", "нет, удали"} {
		if Confirmation(text) != "" {
			t.Fatalf("ambiguous confirmation: %s", text)
		}
	}
	item := expenses.Expense{ID: 1, UserID: 111, Version: 1}
	repo := &memoryRepo{item: item}
	state := State{Pending: &Pending{ID: 1, Version: 1}, Context: Conversation{Shown: []expenses.Expense{item}}}
	reply, next, err := (Engine{}).Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Text: "кофе 350", State: state})
	if err != nil || next.Pending == nil || repo.deleted || !strings.Contains(reply, "не записана") {
		t.Fatalf("unexpected pending behavior: %s %v", reply, err)
	}
	_, next, err = (Engine{}).Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Text: "да, конечно", State: next})
	if err != nil || !repo.deleted || len(next.Context.Shown) != 0 {
		t.Fatal("deleted expense remains in context")
	}
}
