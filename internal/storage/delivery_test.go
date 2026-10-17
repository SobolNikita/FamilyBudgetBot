package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"familyexpenses/internal/bot"
	"github.com/jackc/pgx/v5"
)

func TestDeliveryDoesNotBlockProcessingAndPreservesDiagnostics(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	for _, updateID := range []int64{10, 11} {
		if err := repo.Enqueue(ctx, Update{ID: updateID, UserID: 111, ChatID: 111, MessageID: updateID + 100, Text: "кофе"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.NextUpdate(ctx)
	if err != nil || first.ID != 10 || first.MessageID != 110 {
		t.Fatalf("wrong first update: %+v %v", first, err)
	}
	if err := repo.Acknowledge(ctx, 10, 1000); err != nil {
		t.Fatal(err)
	}
	next, err := repo.NextDelivery(ctx)
	if err != nil || next.ID != 11 {
		t.Fatalf("pending LLM blocked next receipt: %+v %v", next, err)
	}
	if err := repo.FailProcessing(ctx, 10, "Не выполнено", "llm_timeout"); err != nil {
		t.Fatal(err)
	}
	next, err = repo.NextUpdate(ctx)
	if err != nil || next.ID != 11 {
		t.Fatal("delivery blocked processing")
	}
	if err := repo.Acknowledge(ctx, 11, 1001); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if err := repo.RetryDelivery(ctx, first, 90*time.Second, "telegram_http_429"); err != nil {
			t.Fatal(err)
		}
	}
	var status, code string
	var attempts int
	var delay bool
	if err := pool.QueryRow(ctx, `SELECT status,last_error,send_attempts,delivery_next_attempt_at>now()+interval '80 seconds' FROM telegram_updates WHERE update_id=10`).Scan(&status, &code, &attempts, &delay); err != nil {
		t.Fatal(err)
	}
	if status != "processed" || code != "llm_timeout" || attempts != 8 || !delay {
		t.Fatal("reply lost or retry policy violated")
	}
	if _, err := pool.Exec(ctx, `UPDATE telegram_updates SET created_at=now()-interval '8 days' WHERE update_id=10`); err != nil {
		t.Fatal(err)
	}
	if err := repo.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkSent(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_error FROM telegram_updates WHERE update_id=10`).Scan(&code); err != nil || code != "llm_timeout" {
		t.Fatal("diagnostic removed after delivery")
	}
	if err := repo.Complete(ctx, 11, 111, "Готово", bot.State{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.FailProcessing(ctx, 11, "Ошибка", "llm_timeout"); err != nil {
		t.Fatal(err)
	}
	next, err = repo.NextDelivery(ctx)
	if err != nil || next.ID != 11 || next.Reply != "Готово" {
		t.Fatal("failure overwrote committed result")
	}
	if err := repo.MarkSent(ctx, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.NextDelivery(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("delivered replies still queued")
	}
}
