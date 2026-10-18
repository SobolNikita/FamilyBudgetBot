package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
)

type independentPlanner struct {
	entered chan struct{}
	other   chan string
	active  atomic.Int32
}

func (p *independentPlanner) Plan(ctx context.Context, input llm.Request) (llm.Action, error) {
	p.active.Add(1)
	defer p.active.Add(-1)
	if input.CurrentUser == "Slow" {
		select {
		case p.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return llm.Action{}, ctx.Err()
	}
	p.other <- input.Today
	return llm.Action{Name: "ask_question", Subject: "self", Question: "ok"}, nil
}
func TestDifferentUsersProgressAndRunnerCancelsAllWork(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,display_name) VALUES(111,'Slow'),(222,'Fast')`); err != nil {
		t.Fatal(err)
	}
	for _, update := range []storage.Update{{ID: 1, UserID: 111, ChatID: 111, Text: "slow"}, {ID: 2, UserID: 111, ChatID: 111, Text: "second"}, {ID: 3, UserID: 222, ChatID: 222, Text: "yesterday", MessageAt: time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)}} {
		if err := repo.Enqueue(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/getUpdates" {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
	}))
	defer server.Close()
	planner := &independentPlanner{entered: make(chan struct{}, 2), other: make(chan string, 2)}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	runner := Runner{Pool: pool, Client: &Client{BaseURL: server.URL, HTTP: server.Client()}, Planner: planner, Engine: bot.Engine{Currency: "RUB"}, Location: time.FixedZone("Moscow", 3*3600)}
	go func() { done <- runner.Run(runCtx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("runner did not terminate")
		}
	}()
	select {
	case <-planner.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("slow user did not start")
	}
	select {
	case day := <-planner.other:
		if day != "2026-10-16" {
			t.Fatalf("wrong message date %s", day)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow user blocked another user")
	}
	select {
	case <-planner.entered:
		t.Fatal("same user processed concurrently")
	default:
	}
	cancel()
	deadline := time.After(3 * time.Second)
	for planner.active.Load() != 0 {
		select {
		case <-deadline:
			t.Fatal("planner goroutine did not cancel")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPollingBackoffHonorsRetryAfterAndPermanentErrors(t *testing.T) {
	delay, _ := pollRetry(&APIError{Status: 429, RetryAfter: 90 * time.Second}, 1)
	if delay < 90*time.Second {
		t.Fatal("retry_after ignored")
	}
	delay, code := pollRetry(&APIError{Status: 401}, 1)
	if delay < time.Minute || code != "telegram_http_401" {
		t.Fatal("permanent polling failure retried too quickly")
	}
}
