package telegram

import (
	"context"
	"familyexpenses/internal/bot"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type removalPlanner struct{ entered, release chan struct{} }

func (p *removalPlanner) Plan(ctx context.Context, _ llm.Request) (llm.Action, error) {
	close(p.entered)
	select {
	case <-p.release:
	case <-ctx.Done():
		return llm.Action{}, ctx.Err()
	}
	return llm.Action{Name: "create_expenses", Subject: "self", Expenses: []llm.NewExpense{{Amount: "350", Category: "еда", SpentAt: "2026-10-18", Comment: "coffee"}}}, nil
}

func TestRemovalDuringLLMDoesNotResurrectExpenseOrDialog(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,display_name) VALUES(111,'Member')`); err != nil {
		t.Fatal(err)
	}
	update := storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "coffee 350"}
	if err := repo.Enqueue(ctx, update); err != nil {
		t.Fatal(err)
	}
	planner := &removalPlanner{entered: make(chan struct{}), release: make(chan struct{})}
	runner := Runner{Pool: pool, Planner: planner, Engine: bot.Engine{Currency: "RUB"}, Location: time.UTC}
	done := make(chan error, 1)
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go func() { done <- runner.process(runCtx, repo, update) }()
	select {
	case <-planner.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("planner did not start")
	}
	if err := repo.RemoveUser(ctx, 111, 1); err != nil {
		t.Fatal(err)
	}
	close(planner.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM expenses)+(SELECT count(*) FROM dialog_states)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("late processing recreated data: %d %v", count, err)
	}
	if err := repo.RestoreUser(ctx, 111, 2); err != nil {
		t.Fatal(err)
	}
	runner.Planner = forbiddenPlanner{testing: t}
	if err := runner.process(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	next := storage.Update{ID: 2, UserID: 111, ChatID: 111, Text: "привет"}
	if err := repo.Enqueue(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := runner.process(ctx, repo, next); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id=111`).Scan(&name); err != nil || name != "" {
		t.Fatal("old name restored")
	}
}

func TestRemovalSerializesWithDeliveryAndDiscardsStaleSnapshot(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	if err := repo.Enqueue(ctx, storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "old private text"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.FailProcessing(ctx, 1, "old private reply", ""); err != nil {
		t.Fatal(err)
	}
	update, err := repo.NextDelivery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
	}))
	defer server.Close()
	runner := Runner{Client: &Client{BaseURL: server.URL, HTTP: server.Client()}}
	delivery := make(chan error, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	go func() { delivery <- runner.deliverUpdate(ctx, repo, update) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery did not start")
	}
	removed := make(chan error, 1)
	go func() { removed <- repo.RemoveUser(ctx, 111, 1) }()
	select {
	case err := <-removed:
		t.Fatalf("removal crossed active send: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-delivery; err != nil {
		t.Fatal(err)
	}
	if err := <-removed; err != nil {
		t.Fatal(err)
	}
	if err := runner.deliverUpdate(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("stale delivery was sent after removal")
	}
}
