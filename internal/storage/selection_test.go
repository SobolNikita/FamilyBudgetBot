package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"familyexpenses/internal/expenses"
)

func TestSelectionDeletesAllPagesAndOnlyMatchingFilter(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	// 45 matching records, with exclusions for each supported predicate.
	_, err := pool.Exec(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at,comment)
	 SELECT 111,1,'еда','2026-10-18','кофе' FROM generate_series(1,45);
	 INSERT INTO expenses(user_id,amount,category,spent_at,comment) VALUES
	 (222,1,'еда','2026-10-18','кофе'),(111,1,'другое','2026-10-18','кофе'),
	 (111,1,'еда','2026-10-17','кофе'),(111,1,'еда','2026-10-19','кофе'),
	 (111,1,'еда','2026-10-18','чай')`)
	if err != nil {
		t.Fatal(err)
	}
	filter := expenses.Filter{UserID: 111, Category: "еда", From: "2026-10-18", To: "2026-10-18", Search: "кофе", Limit: 20, Offset: 20}
	selection, err := repo.SelectExpenses(ctx, filter)
	if err != nil || selection.Count != 45 {
		t.Fatalf("selection: %+v %v", selection, err)
	}
	filter.Offset = 0
	otherPage, err := repo.SelectExpenses(ctx, filter)
	if err != nil || otherPage != selection {
		t.Fatalf("selection depends on page: %+v %v", otherPage, err)
	}
	count, err := repo.DeleteSelection(ctx, filter, selection.Token)
	if err != nil || count != 45 {
		t.Fatalf("delete: %d %v", count, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&remaining); err != nil || remaining != 5 {
		t.Fatalf("remaining: %d %v", remaining, err)
	}
	if _, err := repo.DeleteSelection(ctx, filter, selection.Token); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("replay: %v", err)
	}
	// New records are never captured by replaying a previously confirmed request.
	if _, err := pool.Exec(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at,comment) VALUES(111,1,'еда','2026-10-18','кофе')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteSelection(ctx, filter, selection.Token); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("replay with new record: %v", err)
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT expense_revision FROM users WHERE id=111`).Scan(&revision); err != nil || revision <= 45 {
		t.Fatalf("bot revision: %d %v", revision, err)
	}
}

func TestSelectionConflictIsAtomic(t *testing.T) {
	for _, mutation := range []string{
		`INSERT INTO expenses(user_id,amount,category,spent_at) VALUES(111,2,'еда','2026-10-18')`,
		`UPDATE expenses SET amount=2,version=version+1 WHERE id=(SELECT min(id) FROM expenses)`,
		`UPDATE expenses SET user_id=222,version=version+1 WHERE id=(SELECT min(id) FROM expenses)`,
		`DELETE FROM expenses WHERE id=(SELECT min(id) FROM expenses)`,
	} {
		t.Run(mutation, func(t *testing.T) {
			pool := testDatabase(t)
			ctx := context.Background()
			repo := Repository{DB: pool}
			if _, err := pool.Exec(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at) SELECT 111,1,'еда','2026-10-18' FROM generate_series(1,3)`); err != nil {
				t.Fatal(err)
			}
			filter := expenses.Filter{UserID: 111}
			before, err := repo.SelectExpenses(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			changed, err := repo.SelectExpenses(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.DeleteSelection(ctx, filter, before.Token); !errors.Is(err, expenses.ErrConflict) {
				t.Fatalf("expected conflict: %v", err)
			}
			after, err := repo.SelectExpenses(ctx, filter)
			if err != nil || after != changed {
				t.Fatalf("partial deletion: %+v %+v %v", changed, after, err)
			}
		})
	}
}

func TestSelectionWaitsForWritesAndRejectsStaleSnapshot(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	if _, err := pool.Exec(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at) VALUES(111,1,'еда','2026-10-18')`); err != nil {
		t.Fatal(err)
	}
	selection, err := repo.SelectExpenses(ctx, expenses.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer Rollback(tx)
	if err := LockLifecycle(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE expenses SET amount=2,version=version+1`); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := repo.DeleteSelection(short, expenses.Filter{}, selection.Token); err == nil {
		t.Fatal("deletion bypassed in-flight write")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteSelection(ctx, expenses.Filter{}, selection.Token); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("stale snapshot accepted: %v", err)
	}
	if _, err := repo.DeleteSelection(ctx, expenses.Filter{}, ""); !errors.Is(err, expenses.ErrInvalid) {
		t.Fatalf("empty token accepted: %v", err)
	}
	if _, err := repo.SelectExpenses(ctx, expenses.Filter{Category: "invalid"}); !errors.Is(err, expenses.ErrInvalid) {
		t.Fatalf("invalid filter accepted: %v", err)
	}
}
