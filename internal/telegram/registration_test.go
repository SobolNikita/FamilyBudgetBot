package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

type forbiddenPlanner struct{ testing *testing.T }

func (planner forbiddenPlanner) Plan(context.Context, llm.Request) (llm.Action, error) {
	planner.testing.Fatal("registration must not call LLM")
	return llm.Action{}, nil
}

func TestRegistrationPersistsAndDoesNotReplayNames(t *testing.T) {
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
	schema := fmt.Sprintf("test_registration_%d", time.Now().UnixNano())
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
	repo := storage.Repository{DB: pool}
	first := storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "кофе 350"}
	runner := Runner{Pool: pool, Planner: forbiddenPlanner{testing: t}}
	if err := repo.Enqueue(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := runner.process(ctx, repo, first); err != nil {
		t.Fatal(err)
	}
	var reply string
	if err := pool.QueryRow(ctx, `SELECT reply_text FROM telegram_updates WHERE update_id=1`).Scan(&reply); err != nil || !strings.Contains(reply, "Как вас зовут?") {
		t.Fatalf("name was not requested: %s %v", reply, err)
	}
	users, err := repo.Users(ctx)
	if err != nil || len(users) != 0 {
		t.Fatal("unfinished participant shown in panel")
	}
	if err := repo.MarkSent(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE dialog_states SET expires_at=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if err := repo.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := Runner{Pool: pool, Planner: forbiddenPlanner{testing: t}}
	name := storage.Update{ID: 2, UserID: 111, ChatID: 111, Text: "  Анна  "}
	if err := repo.Enqueue(ctx, name); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := restarted.process(ctx, repo, name); err != nil {
			t.Fatal(err)
		}
	}
	users, err = repo.Users(ctx)
	if err != nil || len(users) != 1 || users[0].DisplayName != "Анна" {
		t.Fatalf("registration did not persist: %+v %v", users, err)
	}
	var expensesCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&expensesCount); err != nil || expensesCount != 0 {
		t.Fatal("registration created an expense")
	}
	if err := repo.MarkSent(ctx, name.ID); err != nil {
		t.Fatal(err)
	}
	expense := storage.Update{ID: 3, UserID: 111, ChatID: 111, Text: "кофе 350"}
	if err := repo.Enqueue(ctx, expense); err != nil {
		t.Fatal(err)
	}
	handled, err := restarted.register(ctx, expense)
	if err != nil || handled {
		t.Fatal("registered participant forced to register again")
	}
}
