package bot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

type batchRepo struct {
	expenses.Repository
	created []expenses.Expense
}

func (repo *batchRepo) Create(_ context.Context, input expenses.Input) (expenses.Expense, error) {
	item := expenses.Expense{ID: int64(len(repo.created) + 1), UserID: input.UserID, Amount: input.Amount, Category: input.Category, SpentAt: input.SpentAt, Comment: input.Comment, Version: 1}
	repo.created = append(repo.created, item)
	return item, nil
}

func TestBatchCreationPreservesEveryItemAndContext(t *testing.T) {
	repo := &batchRepo{}
	items := []llm.NewExpense{
		{Amount: "5", Category: "еда", SpentAt: "2026-10-17", Comment: "Чипсы"},
		{Amount: "8,20", Category: "еда", SpentAt: "2026-10-17", Comment: "Йогурт"},
		{Amount: "5", Category: "еда", SpentAt: "2026-10-17", Comment: "Чипсы"},
	}
	reply, state, err := (Engine{Currency: "BYN"}).Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Text: "Чипсы 5 Йогурт 8,20 Чипсы 5", Action: llm.Action{Name: "create_expenses", Expenses: items}})
	if err != nil || len(repo.created) != 3 {
		t.Fatalf("batch incomplete: %+v %v", repo.created, err)
	}
	for _, item := range repo.created {
		if item.UserID != 111 || !strings.Contains(reply, item.Comment) {
			t.Fatal("ownership or reply incorrect")
		}
	}
	if repo.created[1].Amount != "8.20" || strings.Count(reply, "Чипсы") != 2 {
		t.Fatal("money or duplicate purchases lost")
	}
	if len(state.Context.Shown) != 3 || state.Context.LastExpense == nil || state.Context.LastExpense.ID != 3 || len(state.Context.Turns) != 1 {
		t.Fatal("batch context incomplete")
	}
}

func TestBatchSizeValidation(t *testing.T) {
	for _, count := range []int{0, llm.MaxExpensesPerMessage + 1} {
		repo := &batchRepo{}
		_, _, err := (Engine{}).Apply(context.Background(), expenses.Service{Repo: repo}, 111, Prepared{Action: llm.Action{Name: "create_expenses", Expenses: make([]llm.NewExpense, count)}})
		if !errors.Is(err, expenses.ErrInvalid) || len(repo.created) != 0 {
			t.Fatal("invalid batch size accepted")
		}
	}
}
