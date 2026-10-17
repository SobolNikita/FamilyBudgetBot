package bot

import (
	"context"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
	"testing"
)

type listRepo struct{ *memoryRepo }

func (repo listRepo) List(context.Context, expenses.Filter) (expenses.Page, error) {
	return expenses.Page{Items: []expenses.Expense{repo.item}, Total: 1}, nil
}
func TestRefreshListReplacesStaleLastExpense(t *testing.T) {
	old := expenses.Expense{ID: 1, UserID: 111, Amount: "5.00", Category: "еда", SpentAt: "2026-10-17", Version: 1}
	fresh := old
	fresh.Version = 2
	repo := listRepo{&memoryRepo{item: fresh}}
	service := expenses.Service{Repo: repo}
	engine := Engine{Currency: "BYN"}
	_, state, err := engine.Apply(context.Background(), service, 111, Prepared{State: State{Context: Conversation{LastExpense: &old}}, Action: llm.Action{Name: "list_expenses", Subject: "self"}})
	if err != nil {
		t.Fatal(err)
	}
	amount := "7"
	_, _, err = engine.Apply(context.Background(), service, 111, Prepared{State: state, Snapshot: []expenses.Expense{fresh}, Action: llm.Action{Name: "update_expense", Subject: "self", ID: "1", Amount: &amount}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRegistrationBurstRequiresExplicitConfirmation(t *testing.T) {
	candidate := Register(Registration{NameRequested: true}, "такси 10")
	if candidate.DisplayName != "" || candidate.PendingName != "такси 10" {
		t.Fatal("burst was registered without confirmation")
	}
	canceled := Register(Registration{NameRequested: true, PendingName: candidate.PendingName}, "нет")
	if canceled.DisplayName != "" {
		t.Fatal("canceled name saved")
	}
	rename := Register(Registration{DisplayName: "Анна", NameRequested: true}, "изменить имя")
	candidate = Register(Registration{DisplayName: rename.DisplayName, NameRequested: true, PendingName: rename.PendingName}, "Анна-Мария")
	confirmed := Register(Registration{DisplayName: candidate.DisplayName, NameRequested: true, PendingName: candidate.PendingName}, "да")
	if confirmed.DisplayName != "Анна-Мария" {
		t.Fatal("rename did not complete")
	}
}
