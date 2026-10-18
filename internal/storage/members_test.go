package storage

import (
	"context"
	"errors"
	"familyexpenses/internal/dialog"
	"familyexpenses/internal/expenses"
	"testing"
)

func TestMemberRemovalPurgesDataAndRevokesOldRequests(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	service := expenses.Service{Repo: repo}
	input := expenses.Input{UserID: 111, Amount: "12.50", Category: "еда", SpentAt: "2026-10-18", Comment: "private", IdempotencyKey: "member-create-00001"}
	first, err := service.Create(ctx, expenses.FamilyActor, input)
	if err != nil {
		t.Fatal(err)
	}
	other := input
	other.UserID = 222
	other.IdempotencyKey = "member-create-00002"
	transferred, err := service.Create(ctx, expenses.FamilyActor, other)
	if err != nil {
		t.Fatal(err)
	}
	moved := other
	moved.UserID = 111
	moved.Version = transferred.Version
	moved.IdempotencyKey = ""
	if _, err := service.Update(ctx, expenses.FamilyActor, transferred.ID, moved); err != nil {
		t.Fatal(err)
	}
	survivor := other
	survivor.IdempotencyKey = "member-create-00003"
	if _, err := service.Create(ctx, expenses.FamilyActor, survivor); err != nil {
		t.Fatal(err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 1, UserID: 111, ChatID: 111, Text: "private"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, 1, 111, "private reply", dialog.State{Context: dialog.Conversation{LastExpense: &first}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, 999, 222, "", dialog.State{Context: dialog.Conversation{Shown: []expenses.Expense{transferred}}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveUser(ctx, 111, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveUser(ctx, 111, 1); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
	users, err := repo.Users(ctx)
	if err != nil || len(users) != 1 || users[0].ID != 222 {
		t.Fatalf("users=%+v err=%v", users, err)
	}
	removed, err := repo.RemovedUsers(ctx)
	if err != nil || len(removed) != 1 || removed[0].DisplayName != "" || removed[0].Version != 2 {
		t.Fatalf("removed=%+v err=%v", removed, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expenses=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dialog_states`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("dialogs=%d err=%v", count, err)
	}
	var payload, reply, status string
	if err := pool.QueryRow(ctx, `SELECT payload::text,reply_text,status FROM telegram_updates WHERE update_id=1`).Scan(&payload, &reply, &status); err != nil || payload != "{}" || reply != "" || status != "sent" {
		t.Fatalf("unpurged update: %s %s %s %v", payload, reply, status, err)
	}
	for _, old := range []expenses.Input{input, other} {
		if _, err := service.Create(ctx, expenses.FamilyActor, old); !errors.Is(err, expenses.ErrGone) {
			t.Fatalf("old request returned personal data: %v", err)
		}
	}
	fresh := input
	fresh.IdempotencyKey = "member-create-new1"
	if _, err := service.Create(ctx, expenses.FamilyActor, fresh); !errors.Is(err, expenses.ErrInvalid) {
		t.Fatalf("removed user accepted write: %v", err)
	}
	if err := repo.Enqueue(ctx, Update{ID: 2, UserID: 111, ChatID: 111, Text: "secret attempt"}); err != nil {
		t.Fatal(err)
	}
	update, err := repo.NextDelivery(ctx)
	if err != nil || update.Reply != RemovedMemberReply || update.Status != "processed" {
		t.Fatalf("removed user reply=%+v err=%v", update, err)
	}
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM telegram_updates WHERE update_id=2`).Scan(&payload); err != nil || payload != "{}" {
		t.Fatal("blocked message retained")
	}
	if err := repo.RestoreUser(ctx, 111, 2); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_updates WHERE user_id=111 AND status='processed'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("obsolete blocked replies survive restoration")
	}
	if err := repo.RestoreUser(ctx, 111, 2); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveUser(ctx, 111, 1); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("stale delete affected restored user: %v", err)
	}
	if _, err := service.Create(ctx, expenses.FamilyActor, input); !errors.Is(err, expenses.ErrGone) {
		t.Fatalf("old request revived after restoration: %v", err)
	}
	if _, err := repo.User(ctx, 111); !errors.Is(err, expenses.ErrNotFound) {
		t.Fatal("restoration bypassed registration")
	}
}
