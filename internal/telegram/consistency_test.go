package telegram

import (
	"context"
	"familyexpenses/internal/bot"
	"familyexpenses/internal/dialog"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
	"fmt"
	"strings"
	"testing"
	"time"
)

type contextCapturePlanner struct{ request llm.Request }

func (p *contextCapturePlanner) Plan(_ context.Context, request llm.Request) (llm.Action, error) {
	p.request = request
	return llm.Action{Name: "ask_question", Subject: "self", Question: "Уточните запрос"}, nil
}

func TestPaginationRequiresRestartAfterConcurrentMutation(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO users(id,display_name) VALUES(111,'Review')`)
	if err != nil {
		t.Fatal(err)
	}
	repo := storage.Repository{DB: pool}
	service := expenses.Service{Repo: repo}
	var first expenses.Expense
	for i := 0; i < 21; i++ {
		item, err := service.Create(ctx, 111, expenses.Input{Amount: "1", Category: "еда", SpentAt: "2026-10-18", Comment: fmt.Sprint(i)})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = item
		}
	}
	engine := bot.Engine{}
	_, state, err := engine.Apply(ctx, service, 111, bot.Prepared{Action: llm.Action{Name: "list_expenses", Subject: "self"}})
	if err != nil {
		t.Fatal(err)
	}
	visible := state.Context.Shown[0]
	if err := service.Delete(ctx, expenses.FamilyActor, visible.ID, visible.Version); err != nil {
		t.Fatal(err)
	}
	reply, next, err := engine.Apply(ctx, service, 111, bot.Prepared{State: state, Action: llm.Action{Name: "continue_expenses", Subject: "self"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, 111, first.ID); err != nil {
		t.Fatal(err)
	}
	if len(next.Context.Shown) != 0 || next.Context.List != nil || !strings.Contains(reply, "Список расходов изменился") {
		t.Fatalf("silent skip: %s", reply)
	}
	t.Logf("Changed list requires restart: %s", reply)
}

func TestContextDropsTransferredExpenseBeforePlanning(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,display_name) VALUES(111,'Review'),(222,'Other')`); err != nil {
		t.Fatal(err)
	}
	repo := storage.Repository{DB: pool}
	service := expenses.Service{Repo: repo}
	item, err := service.Create(ctx, 111, expenses.Input{Amount: "10", Category: "еда", SpentAt: "2026-10-18", Comment: "old coffee"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, 900, 111, "", dialog.State{Context: dialog.Conversation{LastExpense: &item, Shown: []expenses.Expense{item}}}); err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(ctx, expenses.FamilyActor, item.ID, expenses.Input{UserID: 222, Amount: "20", Category: item.Category, SpentAt: item.SpentAt, Comment: "new coffee", Version: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	update := storage.Update{ID: 901, UserID: 111, ChatID: 111, Text: "что было последнее", MessageAt: time.Now()}
	if err := repo.Enqueue(ctx, update); err != nil {
		t.Fatal(err)
	}
	planner := &contextCapturePlanner{}
	runner := Runner{Pool: pool, Planner: planner, Engine: bot.Engine{}, Location: time.UTC}
	if err := runner.process(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	if len(planner.request.RecentExpenses) != 0 || planner.request.Context.LastExpense != nil || len(planner.request.Context.Shown) != 0 {
		t.Fatal("stale transferred expense reached planner")
	}

}

func TestInvalidFilterPreservesConversationAndContinuation(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,display_name) VALUES(111,'Анна')`); err != nil {
		t.Fatal(err)
	}
	repo := storage.Repository{DB: pool}
	service := expenses.Service{Repo: repo}
	var last expenses.Expense
	for range 21 {
		item, err := service.Create(ctx, 111, expenses.Input{Amount: "1", Category: "еда", SpentAt: "2026-10-18", Comment: "кофе"})
		if err != nil {
			t.Fatal(err)
		}
		last = item
	}
	_, original, err := (bot.Engine{}).Apply(ctx, service, 111, bot.Prepared{Text: "покажи расходы", Action: llm.Action{Name: "list_expenses", Subject: "self"}})
	if err != nil {
		t.Fatal(err)
	}
	original.Context.LastExpense = &last
	original.Context.Turns[0].Date = "2026-10-18"
	if err := repo.Complete(ctx, 900, 111, "", original); err != nil {
		t.Fatal(err)
	}
	planner := &batchPlanner{action: llm.Action{Name: "summarize_expenses", Subject: "self", From: "2026-02-30"}}
	runner := Runner{Pool: pool, Planner: planner, Location: time.UTC}
	update := storage.Update{ID: 901, UserID: 111, ChatID: 111, Text: "сколько за 30 февраля", MessageAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	if err := repo.Enqueue(ctx, update); err != nil {
		t.Fatal(err)
	}
	if err := runner.process(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	state, err := repo.State(ctx, 111)
	if err != nil {
		t.Fatal(err)
	}
	if state.Context.LastExpense == nil || state.Context.LastExpense.ID != last.ID || len(state.Context.Shown) != 20 || state.Context.List == nil || state.Context.List.NextOffset != 20 {
		t.Fatalf("validation erased useful context: %+v", state)
	}
	if len(state.Context.Turns) != 2 || state.Context.Turns[0].Date != "2026-10-18" || state.Context.Turns[1].Date != "2026-10-19" || !strings.Contains(strings.ToLower(state.Context.Turns[1].Reply), "дат") {
		t.Fatalf("validation was not recorded as a separate dated turn: %+v", state.Context.Turns)
	}
	planner.action = llm.Action{Name: "continue_expenses", Subject: "self"}
	update.ID, update.Text = 902, "покажи ещё"
	if err := repo.Enqueue(ctx, update); err != nil {
		t.Fatal(err)
	}
	if err := runner.process(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	state, err = repo.State(ctx, 111)
	if err != nil || len(state.Context.Shown) != 1 || state.Context.List == nil || state.Context.List.NextOffset != 21 {
		t.Fatalf("continuation after invalid filter failed: %+v %v", state, err)
	}
}
