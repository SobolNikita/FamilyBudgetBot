package bot

import (
	"context"
	"errors"
	"testing"

	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

type memoryRepo struct {
	expenses.Repository
	item    expenses.Expense
	deleted bool
}

func (repo *memoryRepo) Get(_ context.Context, actor, id int64) (expenses.Expense, error) {
	if repo.deleted || id != repo.item.ID || (actor != expenses.FamilyActor && actor != repo.item.UserID) {
		return expenses.Expense{}, expenses.ErrNotFound
	}
	return repo.item, nil
}

func (repo *memoryRepo) Delete(ctx context.Context, actor, id int64, version int) error {
	item, err := repo.Get(ctx, actor, id)
	if err != nil {
		return err
	}
	if item.Version != version {
		return expenses.ErrConflict
	}
	repo.deleted = true
	return nil
}

func (repo *memoryRepo) Update(ctx context.Context, actor, id int64, input expenses.Input) (expenses.Expense, error) {
	item, err := repo.Get(ctx, actor, id)
	if err != nil {
		return item, err
	}
	if item.Version != input.Version {
		return item, expenses.ErrConflict
	}
	item.Amount = input.Amount
	item.Category = input.Category
	item.SpentAt = input.SpentAt
	item.Comment = input.Comment
	item.Version++
	repo.item = item
	return item, nil
}

func TestDeleteRequiresConfirmationAndChecksVersion(t *testing.T) {
	ctx := context.Background()
	item := expenses.Expense{ID: 1, UserID: 111, Amount: "10.00", Category: "транспорт", SpentAt: "2026-10-16", Comment: "такси", Version: 1}
	repo := &memoryRepo{item: item}
	service := expenses.Service{Repo: repo}
	engine := Engine{Currency: "RUB"}
	prepared := Prepared{Text: "удали такси", Action: llm.Action{Name: "delete_expense", Subject: "self", ID: "1"}, Snapshot: []expenses.Expense{item}}
	_, state, err := engine.Apply(ctx, service, 111, prepared)
	if err != nil || state.Pending == nil || repo.deleted {
		t.Fatalf("delete did not request confirmation: %+v %v", state, err)
	}
	repo.item.Version++
	_, _, err = engine.Apply(ctx, service, 111, Prepared{Text: "да", State: state})
	if !errors.Is(err, expenses.ErrConflict) || repo.deleted {
		t.Fatal("stale confirmation deleted an updated expense")
	}
	repo.item.Version = 1
	_, final, err := engine.Apply(ctx, service, 111, Prepared{Text: "да", State: state})
	if err != nil || !repo.deleted || final.Pending != nil {
		t.Fatal("confirmed deletion failed")
	}
}

func TestCrossUserAndInventedIDsRejected(t *testing.T) {
	item := expenses.Expense{ID: 1, UserID: 222, Version: 1}
	repo := &memoryRepo{item: item}
	engine := Engine{}
	for _, snapshot := range [][]expenses.Expense{nil, {item}} {
		_, _, err := engine.Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Text: "удали", Action: llm.Action{Name: "delete_expense", Subject: "self", ID: "1"}, Snapshot: snapshot})
		if !errors.Is(err, expenses.ErrInvalid) || repo.deleted {
			t.Fatal("unseen or foreign expense allowed")
		}
	}
}

func TestPartialUpdatePreservesOtherFields(t *testing.T) {
	item := expenses.Expense{ID: 1, UserID: 111, Amount: "350.00", Category: "еда", SpentAt: "2026-10-16", Comment: "кофе", Version: 2}
	repo := &memoryRepo{item: item}
	amount := "450"
	_, state, err := (Engine{Currency: "RUB"}).Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Text: "последнее было 450", Action: llm.Action{Name: "update_expense", Subject: "self", ID: "1", Amount: &amount}, Snapshot: []expenses.Expense{item}})
	if err != nil {
		t.Fatal(err)
	}
	if repo.item.Amount != "450.00" || repo.item.Comment != "кофе" || repo.item.Category != "еда" || repo.item.Version != 3 || state.Context.LastExpense == nil {
		t.Fatalf("bad partial update: %+v", repo.item)
	}
}

func TestPendingDeleteCannotBeOverriddenByModel(t *testing.T) {
	repo := &memoryRepo{item: expenses.Expense{ID: 1, UserID: 111, Version: 1}}
	_, state, err := (Engine{}).Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Text: "нет", State: State{Pending: &Pending{ID: 1, Version: 1}}, Action: llm.Action{Name: "delete_expense", ID: "1"}})
	if err != nil || repo.deleted || state.Pending != nil {
		t.Fatal("cancel did not prevent deletion")
	}
}
