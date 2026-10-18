package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"familyexpenses/internal/dialog"
	"familyexpenses/internal/expenses"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdempotentCreationConcurrentRetryAndDeletedResponse(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	service := expenses.Service{Repo: Repository{DB: pool}}
	input := expenses.Input{IdempotencyKey: "test-create-00000001", UserID: 111, Amount: "5", Category: "еда", SpentAt: "2026-10-17"}
	var group sync.WaitGroup
	results := make(chan expenses.Expense, 16)
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			item, err := service.Create(ctx, expenses.FamilyActor, input)
			if err != nil {
				t.Error(err)
				return
			}
			results <- item
		}()
	}
	group.Wait()
	close(results)
	var first expenses.Expense
	for item := range results {
		if first.ID == 0 {
			first = item
		}
		if item.ID != first.ID {
			t.Fatal("retry created a different expense")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows=%d err=%v", count, err)
	}
	different := input
	different.Amount = "6"
	if _, err := service.Create(ctx, expenses.FamilyActor, different); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("different request accepted: %v", err)
	}
	if err := service.Delete(ctx, expenses.FamilyActor, first.ID, first.Version); err != nil {
		t.Fatal(err)
	}
	replay, err := service.Create(ctx, expenses.FamilyActor, input)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("original response not replayed: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&count); err != nil || count != 0 {
		t.Fatal("retry resurrected deleted expense")
	}
}

func TestPoolWaitIsCanceledAndQueueCapacityDoesNotBlockReplay(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_updates(update_id,user_id,chat_id,payload) SELECT n,111,111,'{"text":"load"}'::jsonb FROM generate_series(1,1000) n`); err != nil {
		t.Fatal(err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 1001, UserID: 222, ChatID: 222, Text: "full"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("unbounded admission: %v", err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 1, UserID: 111, ChatID: 111, Text: "replay"}); err != nil {
		t.Fatal(err)
	}
	config := pool.Config()
	config.MaxConns = 1
	single, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	held, err := single.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	deadline, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	var one int
	err = (TimedDB{DB: single}).QueryRow(deadline, `SELECT 1`).Scan(&one)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool wait not canceled: %v", err)
	}
	held.Release()
	if err := single.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestQueueFairnessAdmissionAndInvalidText(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	if err := repo.Enqueue(ctx, Update{ID: 1, UserID: 111, ChatID: 111, Text: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Acknowledge(ctx, 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, 1, 111, "result", dialog.State{}); err != nil {
		t.Fatal(err)
	}
	for id := int64(2); id <= 22; id++ {
		if err := repo.Enqueue(ctx, Update{ID: id, UserID: 111, ChatID: 111, Text: "new"}); err != nil {
			t.Fatal(err)
		}
	}
	next, err := repo.NextDelivery(ctx)
	if err != nil || next.ID != 1 {
		t.Fatalf("old final starved: %+v %v", next, err)
	}
	var code string
	if err := pool.QueryRow(ctx, `SELECT last_error FROM telegram_updates WHERE update_id=22`).Scan(&code); err != nil || code != "queue_overload" {
		t.Fatalf("admission missing: %s %v", code, err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 23, UserID: 222, ChatID: 222, Text: "bad\x00text"}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_error FROM telegram_updates WHERE update_id=23`).Scan(&code); err != nil || code != "invalid_message" {
		t.Fatal("invalid payload not quarantined")
	}
	if err := repo.Enqueue(ctx, Update{ID: 24, UserID: 222, ChatID: 222, Text: "good"}); err != nil {
		t.Fatal(err)
	}
	next, err = repo.NextUpdate(ctx, 111)
	if err != nil || next.ID != 24 {
		t.Fatalf("other user blocked: %+v %v", next, err)
	}
	if err := repo.TouchProcessing(ctx, 111); err != nil {
		t.Fatal(err)
	}
	next, err = repo.NextUpdate(ctx)
	if err != nil || next.UserID != 222 {
		t.Fatal("dispatch is not fair between users")
	}
	if _, err := repo.Metrics(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPermanentDeliverySuspendsChatAndNewMessageResumes(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	for id := int64(1); id <= 2; id++ {
		if err := repo.Enqueue(ctx, Update{ID: id, UserID: 111, ChatID: 111, Text: "hello"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SuspendDelivery(ctx, Update{ID: 1, UserID: 111}, "telegram_http_403", true); err != nil {
		t.Fatal(err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 3, UserID: 222, ChatID: 222, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	next, err := repo.NextDelivery(ctx)
	if err != nil || next.UserID != 222 {
		t.Fatal("blocked chat monopolized delivery")
	}
	if err := repo.Enqueue(ctx, Update{ID: 4, UserID: 111, ChatID: 111, Text: "back"}); err != nil {
		t.Fatal(err)
	}
	var suspended int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_updates WHERE user_id=111 AND delivery_suspended`).Scan(&suspended); err != nil || suspended != 0 {
		t.Fatal("returning user did not resume delivery")
	}
}

func TestCurrencyCannotRelabelHistory(t *testing.T) {
	repo := Repository{DB: testDatabase(t)}
	ctx := context.Background()
	for range 2 {
		if err := repo.EnsureCurrency(ctx, "RUB"); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.EnsureCurrency(ctx, "USD"); err == nil {
		t.Fatal("currency silently changed")
	}
}

func TestRepositoryRejectsMissingActor(t *testing.T) {
	repo := Repository{}
	ctx := context.Background()
	_, getErr := repo.Get(ctx, 0, 1)
	_, updateErr := repo.Update(ctx, 0, 1, expenses.Input{UserID: 111})
	deleteErr := repo.Delete(ctx, 0, 1, 1)
	for _, err := range []error{getErr, updateErr, deleteErr} {
		if !errors.Is(err, expenses.ErrInvalid) {
			t.Fatalf("missing actor accepted: %v", err)
		}
	}
}

func TestListUsesOneSnapshotDuringConcurrentChanges(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			var id int64
			if err := pool.QueryRow(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at) VALUES(111,1,'еда','2026-10-17') RETURNING id`).Scan(&id); err != nil {
				t.Error(err)
				return
			}
			if _, err := pool.Exec(ctx, `DELETE FROM expenses WHERE id=$1`, id); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	defer func() { <-done }()
	for range 50 {
		page, err := repo.List(ctx, expenses.Filter{UserID: 111, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != len(page.Items) {
			t.Fatal(fmt.Sprintf("inconsistent snapshot: total=%d items=%d", page.Total, len(page.Items)))
		}
	}
}
