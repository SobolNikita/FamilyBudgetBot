package telegram

import (
	"context"
	"testing"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
)

type namePlanner struct{ subject string }

func (p namePlanner) Plan(context.Context, llm.Request) (llm.Action, error) {
	return llm.Action{Name: "change_name", Subject: p.subject}, nil
}

func TestConversationalNameChangePersistsOnlyAfterConfirmation(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,display_name,name_requested_at) VALUES(111,'Анна',now())`); err != nil {
		t.Fatal(err)
	}
	repo := storage.Repository{DB: pool}
	runner := Runner{Pool: pool, Planner: forbiddenPlanner{testing: t}, Engine: bot.Engine{Currency: "BYN"}, Location: time.UTC}
	for i, text := range []string{"/name", "изменить имя", "Аня", "удали", "да"} {
		update := storage.Update{ID: int64(i + 1), UserID: 111, ChatID: 111, Text: text}
		if err := repo.Enqueue(ctx, update); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := runner.process(ctx, repo, update); err != nil {
				t.Fatal(err)
			}
		}
		var name, pending string
		if err := pool.QueryRow(ctx, `SELECT display_name,pending_name FROM users WHERE id=111`).Scan(&name, &pending); err != nil {
			t.Fatal(err)
		}
		if i < 4 && name != "Анна" {
			t.Fatal("name saved without explicit confirmation")
		}
		if i == 0 && pending != "" {
			t.Fatal("slash command starts rename")
		}
		if i == 4 && (name != "Аня" || pending != "") {
			t.Fatal("confirmation not persisted")
		}
	}
}

func TestModelMayStartOnlyOwnNameChange(t *testing.T) {
	for _, subject := range []string{"self", "other", "family", "unclear"} {
		t.Run(subject, func(t *testing.T) {
			pool := telegramTestDatabase(t)
			ctx := context.Background()
			if _, err := pool.Exec(ctx, `INSERT INTO users(id,display_name) VALUES(111,'Анна')`); err != nil {
				t.Fatal(err)
			}
			repo := storage.Repository{DB: pool}
			runner := Runner{Pool: pool, Planner: namePlanner{subject: subject}, Engine: bot.Engine{Currency: "BYN"}, Location: time.UTC}
			update := storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "Хочу, чтобы вы называли меня иначе"}
			if err := repo.Enqueue(ctx, update); err != nil {
				t.Fatal(err)
			}
			if err := runner.process(ctx, repo, update); err != nil {
				t.Fatal(err)
			}
			var name, pending string
			if err := pool.QueryRow(ctx, `SELECT display_name,pending_name FROM users WHERE id=111`).Scan(&name, &pending); err != nil {
				t.Fatal(err)
			}
			if name != "Анна" || (subject == "self" && pending != "?") || (subject != "self" && pending != "") {
				t.Fatalf("wrong rename scope: %s %s", name, pending)
			}
		})
	}
}
