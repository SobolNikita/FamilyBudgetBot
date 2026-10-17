package telegram

import (
	"context"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/storage"
)

func (runner Runner) register(ctx context.Context, update storage.Update) (bool, error) {
	transaction, err := runner.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer transaction.Rollback(context.Background())
	var status string
	if err := transaction.QueryRow(ctx, `SELECT status FROM telegram_updates WHERE update_id=$1 FOR UPDATE`, update.ID).Scan(&status); err != nil {
		return false, err
	}
	if status != "pending" {
		return true, nil
	}
	var registration bot.Registration
	if err := transaction.QueryRow(ctx, `SELECT display_name, name_requested_at IS NOT NULL FROM users WHERE id=$1 FOR UPDATE`, update.UserID).Scan(&registration.DisplayName, &registration.NameRequested); err != nil {
		return false, err
	}
	result := bot.Register(registration, update.Text)
	if !result.Handled {
		return false, nil
	}
	if _, err := transaction.Exec(ctx, `UPDATE users SET display_name=$2, name_requested_at=COALESCE(name_requested_at, now()), updated_at=now() WHERE id=$1`, update.UserID, result.DisplayName); err != nil {
		return false, err
	}
	if err := (storage.Repository{DB: transaction}).Complete(ctx, update.ID, update.UserID, result.Reply, bot.State{}); err != nil {
		return false, err
	}
	return true, transaction.Commit(ctx)
}
