package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Runner struct {
	Pool     *pgxpool.Pool
	Client   *Client
	Planner  llm.Planner
	Engine   bot.Engine
	Location *time.Location
}

func (runner Runner) Run(ctx context.Context) error {
	connection, err := runner.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	var locked bool
	if err := connection.QueryRow(ctx, `SELECT pg_try_advisory_lock(830124820)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("another Telegram worker is already running")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = connection.Exec(cleanup, `SELECT pg_advisory_unlock(830124820)`)
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan struct{})
	go func() { defer close(finished); runner.poll(ctx) }()
	defer func() { cancel(); <-finished }()
	repo := storage.Repository{DB: runner.Pool}
	lastCleanup := time.Time{}
	for ctx.Err() == nil {
		if time.Since(lastCleanup) > time.Hour {
			if err := repo.Cleanup(ctx); err != nil {
				slog.Error("queue cleanup failed")
			}
			lastCleanup = time.Now()
		}
		var alive int
		if err := connection.QueryRow(ctx, `SELECT 1`).Scan(&alive); err != nil {
			return errors.New("Telegram worker lock connection lost")
		}
		update, err := repo.NextUpdate(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			if !pause(ctx, time.Second) {
				break
			}
			continue
		}
		if err != nil {
			slog.Error("queue read failed")
			if !pause(ctx, 3*time.Second) {
				break
			}
			continue
		}
		sending := update.Status == "processed"
		if sending {
			err = runner.Client.Send(ctx, update.ChatID, update.Reply)
			if err == nil {
				err = repo.MarkSent(ctx, update.ID)
			}
		} else {
			err = runner.process(ctx, repo, update)
		}
		if err != nil && ctx.Err() == nil {
			slog.Warn("bot operation failed", "update_id", update.ID, "delivery", sending, "error", err)
			if retryErr := repo.Retry(ctx, update, sending); retryErr != nil {
				slog.Error("queue retry scheduling failed")
				if !pause(ctx, 3*time.Second) {
					break
				}
			}
		}
	}
	return nil
}

func (runner Runner) poll(ctx context.Context) {
	var offset int64
	repo := storage.Repository{DB: runner.Pool}
	for ctx.Err() == nil {
		updates, err := runner.Client.Poll(ctx, offset)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("Telegram polling failed", "error", err)
			}
			if !pause(ctx, 3*time.Second) {
				return
			}
			continue
		}
		for _, update := range updates {
			message := update.Message
			if message != nil && message.Chat.Type == "private" && message.From.ID > 0 && message.Chat.ID == message.From.ID {
				text := strings.TrimSpace(message.Text)
				if text == "" {
					text = "[неподдерживаемый тип сообщения]"
				}
				if err := repo.Enqueue(ctx, storage.Update{ID: update.ID, UserID: message.From.ID, ChatID: message.Chat.ID, Text: text}); err != nil {
					slog.Error("Telegram update persistence failed")
					pause(ctx, 3*time.Second)
					break
				}
			}
			offset = update.ID + 1
		}
	}
}

func (runner Runner) process(ctx context.Context, repo storage.Repository, update storage.Update) error {
	if handled, err := runner.register(ctx, update); handled || err != nil {
		return err
	}
	state, err := repo.State(ctx, update.UserID)
	if err != nil {
		return err
	}
	recent, err := repo.Recent(ctx, update.UserID)
	if err != nil {
		return err
	}
	prepared := bot.Prepared{Text: update.Text, State: state, Snapshot: recent}
	if state.Pending == nil {
		if update.Text == "/start" || update.Text == "[неподдерживаемый тип сообщения]" || utf8.RuneCountInString(update.Text) > 2000 {
			prepared.Action = llm.Action{Name: "ask_question", Question: "Пришлите короткое текстовое сообщение: «кофе 350», «покажи расходы за неделю», «последнее было 450» или «удали такси»."}
		} else {
			prepared.Action, err = runner.Planner.Plan(ctx, llm.Request{Text: update.Text, Today: time.Now().In(runner.Location).Format(time.DateOnly), Currency: runner.Engine.Currency, Context: state.Context, RecentExpenses: recent})
			if err != nil {
				return err
			}
		}
	}
	transaction, err := runner.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(context.Background())
	var status string
	if err := transaction.QueryRow(ctx, `SELECT status FROM telegram_updates WHERE update_id=$1 FOR UPDATE`, update.ID).Scan(&status); err != nil {
		return err
	}
	if status != "pending" {
		return nil
	}
	if _, err := transaction.Exec(ctx, `SAVEPOINT expense_action`); err != nil {
		return err
	}
	txRepo := storage.Repository{DB: transaction}
	reply, nextState, err := runner.Engine.Apply(ctx, expenses.Service{Repo: txRepo}, update.UserID, prepared)
	if err != nil {
		if !errors.Is(err, expenses.ErrInvalid) && !errors.Is(err, expenses.ErrNotFound) && !errors.Is(err, expenses.ErrConflict) {
			return fmt.Errorf("apply bot action: %w", err)
		}
		if _, rollbackErr := transaction.Exec(ctx, `ROLLBACK TO SAVEPOINT expense_action`); rollbackErr != nil {
			return rollbackErr
		}
		reply = "Не выполнено: " + err.Error()
		nextState = bot.State{}
	}
	if err := txRepo.Complete(ctx, update.ID, update.UserID, reply, nextState); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
