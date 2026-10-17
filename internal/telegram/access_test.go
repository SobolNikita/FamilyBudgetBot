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

type accessPlanner struct {
	request llm.Request
	action  llm.Action
}

func (planner *accessPlanner) Plan(_ context.Context, request llm.Request) (llm.Action, error) {
	planner.request = request
	return planner.action, nil
}

func TestForeignReportRefusedInsteadOfSubstitutingOwnReport(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,display_name) VALUES (111,'Никита'),(222,'Ева')`); err != nil {
		t.Fatal(err)
	}
	repo := storage.Repository{DB: pool}
	for _, item := range []expenses.Input{
		{UserID: 111, Amount: "5.00", Category: "еда", SpentAt: "2026-10-16", Comment: "Никита: кофе"},
		{UserID: 222, Amount: "999.00", Category: "еда", SpentAt: "2026-10-16", Comment: "Ева: личная покупка"},
	} {
		if _, err := repo.Create(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	planner := &accessPlanner{}
	runner := Runner{Pool: pool, Planner: planner, Engine: bot.Engine{Currency: "BYN"}, Location: time.UTC}
	scenarios := []struct {
		userID                    int64
		name, text, subject, want string
	}{
		{111, "Никита", "Сколько потратила Ева за неделю", "other", bot.AccessDeniedReply},
		{111, "Никита", "А за месяц?", "other", bot.AccessDeniedReply},
		{111, "Никита", "Сколько потратила вся семья?", "family", bot.AccessDeniedReply},
		{111, "Никита", "Сколько потратил я за неделю?", "self", "Всего: 5.00 BYN"},
		{222, "Ева", "Сколько потратила Ева за неделю?", "self", "Всего: 999.00 BYN"},
		{222, "Ева", "Сколько потратил Никита?", "other", bot.AccessDeniedReply},
	}
	for position, scenario := range scenarios {
		planner.action = llm.Action{Name: "summarize_expenses", Subject: scenario.subject, From: "2026-10-15", To: "2026-10-21"}
		update := storage.Update{ID: int64(position + 1), UserID: scenario.userID, ChatID: scenario.userID, Text: scenario.text}
		if err := repo.Enqueue(ctx, update); err != nil {
			t.Fatal(err)
		}
		if err := runner.process(ctx, repo, update); err != nil {
			t.Fatal(err)
		}
		if planner.request.CurrentUser != scenario.name {
			t.Fatal("wrong sender identity sent to LLM")
		}
		for _, item := range planner.request.RecentExpenses.([]expenses.Expense) {
			if item.UserID != scenario.userID {
				t.Fatal("foreign expenses sent to LLM")
			}
		}
		if position == 1 {
			conversation := planner.request.Context.(bot.Conversation)
			if len(conversation.Turns) != 1 || conversation.Turns[0].Reply != bot.AccessDeniedReply {
				t.Fatal("foreign subject lost from follow-up context")
			}
		}
		var reply, status string
		if err := pool.QueryRow(ctx, `SELECT reply_text,status FROM telegram_updates WHERE update_id=$1`, update.ID).Scan(&reply, &status); err != nil {
			t.Fatal(err)
		}
		if status != "processed" || !strings.HasPrefix(reply, scenario.want) {
			t.Fatalf("incorrect response to %q: %q (%s)", scenario.text, reply, status)
		}
		if scenario.subject != "self" && reply != bot.AccessDeniedReply {
			t.Fatal("report included in refusal")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&count); err != nil || count != 2 {
		t.Fatal("requests changed expenses")
	}
}
