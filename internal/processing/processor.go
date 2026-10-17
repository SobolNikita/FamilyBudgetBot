package processing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Processor owns the transaction and conversation lifecycle, independently of Telegram transport.
type Processor struct {
	Pool      *pgxpool.Pool
	Planner   llm.Planner
	Engine    bot.Engine
	Location  *time.Location
	ReportLLM func(string)
}

func (p Processor) reportLLM(code string) {
	if p.ReportLLM != nil {
		p.ReportLLM(code)
	}
}

func (runner Processor) Process(ctx context.Context, repo storage.Repository, update storage.Update) error {
	if !update.CreatedAt.IsZero() && time.Since(update.CreatedAt) > time.Hour {
		return repo.FailProcessing(ctx, update.ID, "Сообщение слишком долго ожидало обработки. Расходы по нему не изменены. Пришлите запрос заново.", "queue_timeout")
	}
	if handled, err := runner.Register(ctx, update); handled || err != nil {
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
			prepared.Action = llm.Action{Name: "ask_question", Subject: "self", Question: "Пришлите короткое текстовое сообщение: «кофе 350», «покажи расходы за неделю», «последнее было 450» или «удали такси»."}
		} else {
			user, err := repo.User(ctx, update.UserID)
			if err != nil {
				return err
			}
			started := time.Now()
			messageAt := update.MessageAt
			if messageAt.IsZero() {
				messageAt = update.CreatedAt
			}
			if messageAt.IsZero() {
				messageAt = time.Now()
			}
			planCtx, planCancel := context.WithTimeout(ctx, 2*time.Minute)
			prepared.Action, err = runner.Planner.Plan(planCtx, llm.Request{Text: update.Text, Today: messageAt.In(runner.Location).Format(time.DateOnly), Currency: runner.Engine.Currency, CurrentUser: user.DisplayName, Context: state.Context, RecentExpenses: recent})
			planCancel()
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failure := llm.Diagnose(err)
				runner.reportLLM(failure.Kind)
				slog.Warn("LLM request failed", "update_id", update.ID, "code", failure.Kind, "http_status", failure.Status, "duration_ms", time.Since(started).Milliseconds())
				return repo.FailProcessing(ctx, update.ID, failure.UserMessage(), "llm_"+failure.Kind)
			}
			slog.Info("LLM request completed", "update_id", update.ID, "duration_ms", time.Since(started).Milliseconds())
			runner.reportLLM("")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, storage.OperationTimeout)
	defer cancel()
	transaction, err := (storage.TimedDB{DB: runner.Pool}).Begin(ctx)
	if err != nil {
		return err
	}
	defer storage.Rollback(transaction)
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
	txRepo := storage.Repository{DB: storage.TimedDB{DB: transaction}}
	reply, nextState, err := runner.Engine.Apply(ctx, expenses.Service{Repo: txRepo}, update.UserID, prepared)
	if err != nil {
		if !errors.Is(err, expenses.ErrInvalid) && !errors.Is(err, expenses.ErrNotFound) && !errors.Is(err, expenses.ErrConflict) {
			return fmt.Errorf("apply bot action: %w", err)
		}
		if _, rollbackErr := transaction.Exec(ctx, `ROLLBACK TO SAVEPOINT expense_action`); rollbackErr != nil {
			return rollbackErr
		}
		reply = "Не выполнено: " + err.Error()
		if prepared.Action.Name == "create_expenses" {
			reply += ". Ни один расход из сообщения не сохранён. Исправьте список и пришлите его целиком."
		}
		nextState = bot.State{}
	}
	if err := txRepo.Complete(ctx, update.ID, update.UserID, reply, nextState); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (runner Processor) Register(ctx context.Context, update storage.Update) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, storage.OperationTimeout)
	defer cancel()
	transaction, err := runner.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer storage.Rollback(transaction)
	var status string
	if err := transaction.QueryRow(ctx, `SELECT status FROM telegram_updates WHERE update_id=$1 FOR UPDATE`, update.ID).Scan(&status); err != nil {
		return false, err
	}
	if status != "pending" {
		return true, nil
	}
	var registration bot.Registration
	if err := transaction.QueryRow(ctx, `SELECT display_name, name_requested_at IS NOT NULL,pending_name FROM users WHERE id=$1 FOR UPDATE`, update.UserID).Scan(&registration.DisplayName, &registration.NameRequested, &registration.PendingName); err != nil {
		return false, err
	}
	result := bot.Register(registration, update.Text)
	if !result.Handled {
		return false, nil
	}
	if _, err := transaction.Exec(ctx, `UPDATE users SET display_name=$2,pending_name=$3, name_requested_at=COALESCE(name_requested_at, now()), updated_at=now() WHERE id=$1`, update.UserID, result.DisplayName, result.PendingName); err != nil {
		return false, err
	}
	if err := (storage.Repository{DB: transaction}).Complete(ctx, update.ID, update.UserID, result.Reply, bot.State{}); err != nil {
		return false, err
	}
	return true, transaction.Commit(ctx)
}
