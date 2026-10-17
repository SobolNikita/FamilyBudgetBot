package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/expenses"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connectionString := os.Getenv("TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("test_family_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	config, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	paths, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		migration, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, display_name) VALUES (111, 'Мама'), (222, 'Папа')`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestNewParticipantsRemainHiddenUntilRegistration(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	update := Update{ID: 100, UserID: 333, ChatID: 333, Text: "привет"}
	for range 2 {
		if err := repo.Enqueue(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=333`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("participant not deduplicated: %d %v", count, err)
	}
	users, err := repo.Users(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("incomplete participant exposed: %+v %v", users, err)
	}
	if _, err := (expenses.Service{Repo: repo}).Create(ctx, 0, expenses.Input{UserID: 333, Amount: "10", Category: "еда", SpentAt: "2026-10-16"}); !errors.Is(err, expenses.ErrInvalid) {
		t.Fatalf("incomplete participant can own expenses: %v", err)
	}
}

func TestPostgresIsolationMoneyAndVersions(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	service := expenses.Service{Repo: repo}
	first, err := service.Create(ctx, 111, expenses.Input{Amount: "0.10", Category: "еда", SpentAt: "2026-10-15", Comment: "кофе"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, 111, expenses.Input{Amount: "0.20", Category: "еда", SpentAt: "2026-10-16"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, 222, expenses.Input{Amount: "999.00", Category: "транспорт", SpentAt: "2026-10-16"}); err != nil {
		t.Fatal(err)
	}
	summary, err := service.Summary(ctx, 111, expenses.Filter{UserID: 222, From: "2026-10-15", To: "2026-10-16"})
	if err != nil || summary.Amount != "0.30" || summary.Count != 2 {
		t.Fatalf("incorrect exact/scoped total %+v %v", summary, err)
	}
	page, err := service.List(ctx, 111, expenses.Filter{From: "2026-10-16", To: "2026-10-16"})
	if err != nil || page.Total != 1 {
		t.Fatalf("date boundaries %+v %v", page, err)
	}
	if _, err := repo.Get(ctx, 222, first.ID); !errors.Is(err, expenses.ErrNotFound) {
		t.Fatal("foreign expense visible")
	}
	if err := service.Delete(ctx, 222, first.ID, first.Version); !errors.Is(err, expenses.ErrNotFound) {
		t.Fatal("foreign expense deletable")
	}
	input := expenses.Input{Amount: "450", Category: "еда", SpentAt: first.SpentAt, Version: first.Version}
	if _, err := service.Update(ctx, 222, first.ID, input); !errors.Is(err, expenses.ErrNotFound) {
		t.Fatal("foreign expense editable")
	}
	updated, err := service.Update(ctx, 111, first.ID, input)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update failed %+v %v", updated, err)
	}
	if _, err := service.Update(ctx, 111, first.ID, input); !errors.Is(err, expenses.ErrConflict) {
		t.Fatal("stale update allowed")
	}
	if err := service.Delete(ctx, 111, first.ID, 1); !errors.Is(err, expenses.ErrConflict) {
		t.Fatal("stale delete allowed")
	}
	if err := service.Delete(ctx, 111, first.ID, 2); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresQueueDeduplicationAndAtomicity(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	update := Update{ID: 10, UserID: 111, ChatID: 111, Text: "кофе 350"}
	for range 2 {
		if err := repo.Enqueue(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_updates`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate updates persisted: %d %v", count, err)
	}
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	txRepo := Repository{DB: transaction}
	if _, err := (expenses.Service{Repo: txRepo}).Create(ctx, 111, expenses.Input{Amount: "350", Category: "еда", SpentAt: "2026-10-16"}); err != nil {
		t.Fatal(err)
	}
	if err := txRepo.Complete(ctx, 10, 111, "Добавлено", bot.State{}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rolled-back expense remains")
	}
	queued, err := repo.NextUpdate(ctx)
	if err != nil || queued.Status != "pending" {
		t.Fatal("queue status was not rolled back")
	}
	if err := repo.Complete(ctx, 10, 111, "Ответ", bot.State{Pending: &bot.Pending{ID: 5, Version: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RetryProcessing(ctx, update); err != nil {
		t.Fatal(err)
	}
	var reply string
	if err := pool.QueryRow(ctx, `SELECT reply_text FROM telegram_updates WHERE update_id=10`).Scan(&reply); err != nil || reply != "Ответ" {
		t.Fatal("ambiguous commit retry overwrote a completed result")
	}
	state, err := repo.State(ctx, 111)
	if err != nil || state.Pending == nil || state.Pending.ID != 5 {
		t.Fatalf("state not persisted %+v %v", state, err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 11, UserID: 111, ChatID: 111, Text: "ещё кофе"}); err != nil {
		t.Fatal(err)
	}
	queued, err = repo.NextDelivery(ctx)
	if err != nil || queued.ID != 10 || queued.Status != "processed" {
		t.Fatal("unsent reply lost order")
	}
	if err := repo.MarkSent(ctx, 10); err != nil {
		t.Fatal(err)
	}
	queued, err = repo.NextUpdate(ctx)
	if err != nil || queued.ID != 11 {
		t.Fatal("next user message not released")
	}
	if _, err := pool.Exec(ctx, `UPDATE dialog_states SET expires_at=now()-interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	state, err = repo.State(ctx, 111)
	if err != nil || state.Pending != nil {
		t.Fatal("expired confirmation remains active")
	}
	if err := repo.Complete(ctx, 11, 111, "Ответ", bot.State{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkSent(ctx, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.NextUpdate(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("queue is not empty")
	}
}
