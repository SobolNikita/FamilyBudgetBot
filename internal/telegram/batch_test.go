package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
)

type batchPlanner struct {
	action llm.Action
	calls  int
}

func (planner *batchPlanner) Plan(context.Context, llm.Request) (llm.Action, error) {
	planner.calls++
	return planner.action, nil
}

func TestBatchCreationAtomicityAndReplay(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "complete"
		if invalid {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			pool := telegramTestDatabase(t)
			ctx := context.Background()
			if _, err := pool.Exec(ctx, `INSERT INTO users (id,display_name) VALUES (111,'Анна')`); err != nil {
				t.Fatal(err)
			}
			planner := &batchPlanner{action: llm.Action{Name: "create_expenses", Subject: "self", Expenses: []llm.NewExpense{
				{Amount: "5", Category: "еда", SpentAt: "2026-10-17", Comment: "Чипсы"},
				{Amount: "8", Category: "еда", SpentAt: "2026-10-17", Comment: "Йогурт"},
			}}}
			if invalid {
				planner.action.Expenses[1].Amount = "-8"
			}
			repo := storage.Repository{DB: pool}
			update := storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "Чипсы 5 Йогурт 8"}
			if err := repo.Enqueue(ctx, update); err != nil {
				t.Fatal(err)
			}
			runner := Runner{Pool: pool, Planner: planner, Engine: bot.Engine{Currency: "BYN"}, Location: time.UTC}
			for range 2 {
				if err := runner.process(ctx, repo, update); err != nil {
					t.Fatal(err)
				}
			}
			if planner.calls != 1 {
				t.Fatal("duplicate update called LLM again")
			}
			page, err := repo.List(ctx, expenses.Filter{UserID: 111, Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			var status, reply string
			if err := pool.QueryRow(ctx, `SELECT status,reply_text FROM telegram_updates WHERE update_id=1`).Scan(&status, &reply); err != nil {
				t.Fatal(err)
			}
			if status != "processed" {
				t.Fatal("result not queued for delivery")
			}
			state, err := repo.State(ctx, 111)
			if err != nil {
				t.Fatal(err)
			}
			if invalid {
				if page.Total != 0 || !strings.Contains(reply, "пока не записаны") || state.Context.LastExpense != nil {
					t.Fatalf("partial write survived: %+v %s", page, reply)
				}
			} else {
				if page.Total != 2 || !strings.Contains(reply, "Чипсы") || !strings.Contains(reply, "Йогурт") || !strings.Contains(reply, "5.00") || !strings.Contains(reply, "8.00") {
					t.Fatalf("items missing or duplicated: %+v %s", page, reply)
				}
				summary, err := repo.Summary(ctx, expenses.Filter{UserID: 111})
				if err != nil || summary.Amount != "13.00" {
					t.Fatalf("wrong total: %+v %v", summary, err)
				}
				if len(state.Context.Shown) != 2 || state.Context.LastExpense == nil || state.Context.LastExpense.Comment != "Йогурт" {
					t.Fatal("batch context not saved")
				}
			}
		})
	}
}
