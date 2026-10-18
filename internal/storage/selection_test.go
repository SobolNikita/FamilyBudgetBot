package storage

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"familyexpenses/internal/expenses"
)

func TestManualSelectionDeletesOnlyConfirmedVersions(t *testing.T) {
	for _, change := range []string{"none", "unselected", "edit", "delete", "transfer", "outside-filter"} {
		t.Run(change, func(t *testing.T) {
			pool := testDatabase(t)
			ctx := context.Background()
			repo := Repository{DB: pool}
			var ids []int64
			for range 3 {
				item, err := repo.Create(ctx, expenses.Input{UserID: 111, Amount: "1", Category: "еда", SpentAt: "2026-10-18"})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, item.ID)
			}
			input := expenses.DeleteSelectionRequest{Items: []expenses.ExpenseRef{{ID: ids[0], Version: 1}, {ID: ids[2], Version: 1}}}
			filter := expenses.Filter{UserID: 111}
			var err error
			switch change {
			case "unselected":
				_, err = pool.Exec(ctx, `UPDATE expenses SET version=version+1 WHERE id=$1`, ids[1])
			case "edit":
				_, err = pool.Exec(ctx, `UPDATE expenses SET version=version+1 WHERE id=$1`, ids[2])
			case "delete":
				_, err = pool.Exec(ctx, `DELETE FROM expenses WHERE id=$1`, ids[2])
			case "transfer":
				_, err = pool.Exec(ctx, `UPDATE expenses SET user_id=222,version=version+1 WHERE id=$1`, ids[2])
			case "outside-filter":
				filter.UserID = 222
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := repo.SelectExpenses(ctx, expenses.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			count, err := repo.DeleteSelection(ctx, filter, input)
			if change == "none" || change == "unselected" {
				if err != nil || count != 2 {
					t.Fatalf("delete: %d %v", count, err)
				}
				if _, err := repo.Get(ctx, expenses.FamilyActor, ids[1]); err != nil {
					t.Fatalf("unselected expense removed: %v", err)
				}
				if _, err := repo.DeleteSelection(ctx, filter, input); !errors.Is(err, expenses.ErrConflict) {
					t.Fatalf("replay: %v", err)
				}
			} else {
				if !errors.Is(err, expenses.ErrConflict) {
					t.Fatalf("expected conflict: %v", err)
				}
				after, err := repo.SelectExpenses(ctx, expenses.Filter{})
				if err != nil || before != after {
					t.Fatalf("partial delete: %+v %+v %v", before, after, err)
				}
			}
		})
	}
}

func TestAllSelectionWithExclusions(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	repo := Repository{DB: pool}
	if _, err := pool.Exec(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at) SELECT 111,1,'еда','2026-10-18' FROM generate_series(1,45)`); err != nil {
		t.Fatal(err)
	}
	var first, last int64
	if err := pool.QueryRow(ctx, `SELECT min(id),max(id) FROM expenses`).Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	selection, err := repo.SelectExpenses(ctx, expenses.Filter{UserID: 111})
	if err != nil {
		t.Fatal(err)
	}
	input := expenses.DeleteSelectionRequest{Token: selection.Token, Exclude: []string{strconv.FormatInt(first, 10), strconv.FormatInt(last, 10)}}
	invalid := expenses.DeleteSelectionRequest{Token: selection.Token, Exclude: []string{"9223372036854775807"}}
	if _, err := repo.DeleteSelection(ctx, expenses.Filter{UserID: 111}, invalid); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("invalid exclusion: %v", err)
	}
	count, err := repo.DeleteSelection(ctx, expenses.Filter{UserID: 111}, input)
	if err != nil || count != 43 {
		t.Fatalf("delete: %d %v", count, err)
	}
	for _, id := range []int64{first, last} {
		if _, err := repo.Get(ctx, expenses.FamilyActor, id); err != nil {
			t.Fatalf("excluded expense deleted: %v", err)
		}
	}
	remaining, err := repo.SelectExpenses(ctx, expenses.Filter{})
	if err != nil || remaining.Count != 2 {
		t.Fatalf("remaining: %+v %v", remaining, err)
	}
}

func TestSelectionRequestValidation(t *testing.T) {
	validToken := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, input := range []expenses.DeleteSelectionRequest{
		{}, {Token: "bad"}, {Items: []expenses.ExpenseRef{{ID: 1, Version: 0}}},
		{Items: []expenses.ExpenseRef{{ID: 1, Version: 1}, {ID: 1, Version: 2}}},
		{Token: validToken, Items: []expenses.ExpenseRef{{ID: 1, Version: 1}}},
		{Token: validToken, Exclude: []string{"1", "01"}},
		{Token: validToken, Exclude: []string{"-1"}},
		{Token: validToken, Exclude: []string{"9223372036854775808"}},
		{Exclude: []string{"1"}},
		{Items: make([]expenses.ExpenseRef, expenses.MaxSelectionItems+1)},
		{Token: validToken, Exclude: make([]string, expenses.MaxSelectionItems+1)},
	} {
		if _, _, err := validateSelection(input); !errors.Is(err, expenses.ErrInvalid) {
			t.Fatalf("accepted invalid selection: %+v %v", input, err)
		}
	}
}

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
	count, err := repo.DeleteSelection(ctx, filter, expenses.DeleteSelectionRequest{Token: selection.Token})
	if err != nil || count != 45 {
		t.Fatalf("delete: %d %v", count, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&remaining); err != nil || remaining != 5 {
		t.Fatalf("remaining: %d %v", remaining, err)
	}
	if _, err := repo.DeleteSelection(ctx, filter, expenses.DeleteSelectionRequest{Token: selection.Token}); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("replay: %v", err)
	}
	// New records are never captured by replaying a previously confirmed request.
	if _, err := pool.Exec(ctx, `INSERT INTO expenses(user_id,amount,category,spent_at,comment) VALUES(111,1,'еда','2026-10-18','кофе')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteSelection(ctx, filter, expenses.DeleteSelectionRequest{Token: selection.Token}); !errors.Is(err, expenses.ErrConflict) {
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
			if _, err := repo.DeleteSelection(ctx, filter, expenses.DeleteSelectionRequest{Token: before.Token}); !errors.Is(err, expenses.ErrConflict) {
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
	if _, err := repo.DeleteSelection(short, expenses.Filter{}, expenses.DeleteSelectionRequest{Token: selection.Token}); err == nil {
		t.Fatal("deletion bypassed in-flight write")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteSelection(ctx, expenses.Filter{}, expenses.DeleteSelectionRequest{Token: selection.Token}); !errors.Is(err, expenses.ErrConflict) {
		t.Fatalf("stale snapshot accepted: %v", err)
	}
	if _, err := repo.DeleteSelection(ctx, expenses.Filter{}, expenses.DeleteSelectionRequest{}); !errors.Is(err, expenses.ErrInvalid) {
		t.Fatalf("empty token accepted: %v", err)
	}
	if _, err := repo.SelectExpenses(ctx, expenses.Filter{Category: "invalid"}); !errors.Is(err, expenses.ErrInvalid) {
		t.Fatalf("invalid filter accepted: %v", err)
	}
}
