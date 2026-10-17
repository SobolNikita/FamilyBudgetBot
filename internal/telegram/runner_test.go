package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
)

type blockedPlanner struct {
	release chan struct{}
	calls   atomic.Int32
}

func (planner *blockedPlanner) Plan(ctx context.Context, request llm.Request) (llm.Action, error) {
	if planner.calls.Add(1) == 1 {
		select {
		case <-planner.release:
			return llm.Action{}, &llm.Failure{Kind: "timeout"}
		case <-ctx.Done():
			return llm.Action{}, ctx.Err()
		}
	}
	amount, category, spentAt := "5.00", "еда", "2026-10-17"
	return llm.Action{Name: "create_expense", Subject: "self", Amount: &amount, Category: &category, SpentAt: &spentAt}, nil
}

func TestRunnerAcknowledgesBurstWhileLLMIsBlocked(t *testing.T) {
	pool := telegramTestDatabase(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id,display_name) VALUES (111,'Анна')`); err != nil {
		t.Fatal(err)
	}
	planner := &blockedPlanner{release: make(chan struct{})}
	receipts := make(chan int64, 3)
	edits := make(chan string, 3)
	var polled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Text      string `json:"text"`
			MessageID int64  `json:"message_id"`
			Reply     struct {
				MessageID int64 `json:"message_id"`
			} `json:"reply_parameters"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch request.URL.Path {
		case "/getUpdates":
			if !polled.Swap(true) {
				fmt.Fprint(writer, `{"ok":true,"result":[{"update_id":1,"message":{"message_id":11,"text":"кофе 5","from":{"id":111},"chat":{"id":111,"type":"private"}}},{"update_id":2,"message":{"message_id":12,"text":"чай 5","from":{"id":111},"chat":{"id":111,"type":"private"}}},{"update_id":3,"message":{"message_id":13,"text":"сок 5","from":{"id":111},"chat":{"id":111,"type":"private"}}}]}`)
				return
			}
			<-request.Context().Done()
		case "/sendMessage":
			if body.Text != receiptText {
				t.Error("expected receipt while LLM is blocked")
			}
			receipts <- body.Reply.MessageID
			fmt.Fprintf(writer, `{"ok":true,"result":{"message_id":%d}}`, body.Reply.MessageID+100)
		case "/editMessageText":
			if body.MessageID < 111 || body.MessageID > 113 {
				t.Error("edit not correlated")
			}
			edits <- body.Text
			fmt.Fprint(writer, `{"ok":true,"result":{"message_id":111}}`)
		default:
			t.Error("unexpected Telegram method")
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runner := Runner{Pool: pool, Client: &Client{BaseURL: server.URL, HTTP: server.Client()}, Planner: planner, Engine: bot.Engine{Currency: "BYN"}, Location: time.UTC}
	finished := make(chan error, 1)
	go func() { finished <- runner.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-finished; err != nil && ctx.Err() == nil {
			t.Error(err)
		}
	}()
	for _, messageID := range []int64{11, 12, 13} {
		select {
		case got := <-receipts:
			if got != messageID {
				t.Fatalf("wrong receipt: %d", got)
			}
		case <-ctx.Done():
			t.Fatal("receipt blocked by LLM")
		}
	}
	if planner.calls.Load() != 1 {
		t.Fatal("messages processed out of order")
	}
	close(planner.release)
	var timeoutReplies int
	for range 3 {
		select {
		case reply := <-edits:
			if strings.Contains(reply, "не ответил вовремя") {
				timeoutReplies++
			}
		case <-ctx.Done():
			t.Fatal("missing final reply")
		}
	}
	if timeoutReplies != 1 {
		t.Fatal("timeout not explained")
	}
	for {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_updates WHERE status='sent'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 3 {
			break
		}
		if !pause(ctx, 10*time.Millisecond) {
			t.Fatal("delivery not persisted")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("incorrect expense count: %d %v", count, err)
	}
	if planner.calls.Load() != 3 {
		t.Fatal("LLM failure was retried")
	}
}

func TestReceiptSurvivesRestartAndDeletedMessage(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	if err := repo.Enqueue(ctx, storage.Update{ID: 1, UserID: 111, ChatID: 111, MessageID: 7, Text: "кофе"}); err != nil {
		t.Fatal(err)
	}
	var sent, edited atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sendMessage" {
			sent.Add(1)
			fmt.Fprint(writer, `{"ok":true,"result":{"message_id":42}}`)
			return
		}
		edited.Add(1)
		writer.WriteHeader(400)
		fmt.Fprint(writer, `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`)
	}))
	defer server.Close()
	runner := Runner{Client: &Client{BaseURL: server.URL, HTTP: server.Client()}}
	update, err := repo.NextDelivery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.deliverUpdate(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	if err := repo.FailProcessing(ctx, 1, "Не выполнено", "llm_timeout"); err != nil {
		t.Fatal(err)
	}
	restarted := Runner{Client: runner.Client}
	update, err = repo.NextDelivery(ctx)
	if err != nil || update.ReplyMessageID != 42 {
		t.Fatal("receipt ID not persisted")
	}
	if err := restarted.deliverUpdate(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	update, err = repo.NextDelivery(ctx)
	if err != nil || update.ReplyMessageID != 0 || update.Status != "processed" {
		t.Fatal("deleted receipt not recoverable")
	}
	if err := restarted.deliverUpdate(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	if sent.Load() != 2 || edited.Load() != 1 {
		t.Fatalf("unexpected delivery calls: send=%d edit=%d", sent.Load(), edited.Load())
	}
}

func TestProcessingFinishesWhileReceiptIsInFlight(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	if err := repo.Enqueue(ctx, storage.Update{ID: 1, UserID: 111, ChatID: 111, MessageID: 7, Text: "кофе"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := repo.FailProcessing(ctx, 1, "Ошибка LLM", "llm_timeout"); err != nil {
			t.Error(err)
		}
		fmt.Fprint(writer, `{"ok":true,"result":{"message_id":42}}`)
	}))
	defer server.Close()
	runner := Runner{Client: &Client{BaseURL: server.URL, HTTP: server.Client()}}
	update, err := repo.NextDelivery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.deliverUpdate(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	update, err = repo.NextDelivery(ctx)
	if err != nil || update.Status != "processed" || update.Reply != "Ошибка LLM" || update.ReplyMessageID != 42 {
		t.Fatalf("receipt marked undelivered result as sent: %+v %v", update, err)
	}
}

func TestExpiredQueueItemDoesNotCallLLM(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	if err := repo.Enqueue(ctx, storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "кофе"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE telegram_updates SET created_at=now()-interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	update, err := repo.NextUpdate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Pool: pool, Planner: forbiddenPlanner{testing: t}}
	if err := runner.process(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	update, err = repo.NextDelivery(ctx)
	if err != nil || update.Status != "processed" || !strings.Contains(update.Reply, "не изменены") {
		t.Fatal("expired message silently dropped")
	}
}
