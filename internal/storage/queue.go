package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/expenses"
	"github.com/jackc/pgx/v5"
)

type Update struct {
	ID             int64
	UserID         int64
	ChatID         int64
	Text           string
	Status         string
	Attempts       int
	SendAttempts   int
	Reply          string
	MessageID      int64
	ReplyMessageID int64
	CreatedAt      time.Time
}

func (repo Repository) Enqueue(ctx context.Context, update Update) error {
	payload, err := json.Marshal(map[string]string{"text": update.Text})
	if err != nil {
		return err
	}
	_, err = repo.DB.Exec(ctx, `WITH participant AS (INSERT INTO users (id, display_name) VALUES ($2, '') ON CONFLICT (id) DO NOTHING) INSERT INTO telegram_updates (update_id, user_id, chat_id, payload, message_id) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (update_id) DO NOTHING`, update.ID, update.UserID, update.ChatID, payload, update.MessageID)
	return err
}

func (repo Repository) NextUpdate(ctx context.Context) (Update, error) {
	return scanUpdate(repo.DB.QueryRow(ctx, `SELECT `+updateColumns+` FROM telegram_updates current WHERE status='pending' AND next_attempt_at <= now() AND NOT EXISTS (SELECT 1 FROM telegram_updates earlier WHERE earlier.user_id=current.user_id AND earlier.update_id<current.update_id AND earlier.status='pending') ORDER BY update_id LIMIT 1`))
}

const updateColumns = `update_id, user_id, chat_id, payload->>'text', status, attempts, send_attempts, reply_text, message_id, reply_message_id, created_at`

func scanUpdate(row pgx.Row) (Update, error) {
	var update Update
	err := row.Scan(&update.ID, &update.UserID, &update.ChatID, &update.Text, &update.Status, &update.Attempts, &update.SendAttempts, &update.Reply, &update.MessageID, &update.ReplyMessageID, &update.CreatedAt)
	return update, err
}

func (repo Repository) NextDelivery(ctx context.Context) (Update, error) {
	return scanUpdate(repo.DB.QueryRow(ctx, `SELECT `+updateColumns+` FROM telegram_updates WHERE (status='processed' OR (status='pending' AND reply_message_id=0)) AND delivery_next_attempt_at <= now() ORDER BY (reply_message_id=0) DESC, update_id LIMIT 1`))
}

func (repo Repository) Acknowledge(ctx context.Context, updateID, messageID int64) error {
	_, err := repo.DB.Exec(ctx, `UPDATE telegram_updates SET reply_message_id=$2,send_attempts=0,delivery_error='' WHERE update_id=$1`, updateID, messageID)
	return err
}

func (repo Repository) FailProcessing(ctx context.Context, updateID int64, reply, code string) error {
	_, err := repo.DB.Exec(ctx, `UPDATE telegram_updates SET status='processed',reply_text=$2,last_error=$3,processed_at=now() WHERE update_id=$1 AND status='pending'`, updateID, reply, code)
	return err
}

func (repo Repository) State(ctx context.Context, userID int64) (bot.State, error) {
	var state bot.State
	var contextJSON, pendingJSON []byte
	err := repo.DB.QueryRow(ctx, `SELECT context, pending_action FROM dialog_states WHERE user_id=$1 AND expires_at>now()`, userID).Scan(&contextJSON, &pendingJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(contextJSON, &state.Context); err != nil {
		return state, err
	}
	if len(pendingJSON) > 0 {
		if err := json.Unmarshal(pendingJSON, &state.Pending); err != nil {
			return state, err
		}
	}
	return state, nil
}

func (repo Repository) Recent(ctx context.Context, userID int64) ([]expenses.Expense, error) {
	rows, err := repo.DB.Query(ctx, `SELECT `+columns+` FROM expenses WHERE user_id=$1 ORDER BY id DESC LIMIT 30`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []expenses.Expense{}
	for rows.Next() {
		item, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repo Repository) Complete(ctx context.Context, updateID, userID int64, reply string, state bot.State) error {
	contextJSON, err := json.Marshal(state.Context)
	if err != nil {
		return err
	}
	var pendingJSON []byte
	if state.Pending != nil {
		pendingJSON, err = json.Marshal(state.Pending)
		if err != nil {
			return err
		}
	}
	_, err = repo.DB.Exec(ctx, `INSERT INTO dialog_states (user_id, context, pending_action, expires_at) VALUES ($1,$2,$3,now()+interval '30 minutes') ON CONFLICT (user_id) DO UPDATE SET context=EXCLUDED.context, pending_action=EXCLUDED.pending_action, expires_at=EXCLUDED.expires_at, updated_at=now()`, userID, contextJSON, pendingJSON)
	if err != nil {
		return err
	}
	_, err = repo.DB.Exec(ctx, `UPDATE telegram_updates SET status='processed',reply_text=$2,processed_at=now(),next_attempt_at=now(),last_error='' WHERE update_id=$1`, updateID, reply)
	return err
}

func (repo Repository) RetryProcessing(ctx context.Context, update Update) error {
	delay := time.Duration(1<<min(update.Attempts+1, 6)) * time.Second
	_, err := repo.DB.Exec(ctx, `UPDATE telegram_updates SET attempts=attempts+1,status=CASE WHEN attempts+1>=3 THEN 'processed' ELSE status END,reply_text=CASE WHEN attempts+1>=3 THEN 'Не удалось обработать сообщение. Попробуйте повторить запрос позже.' ELSE reply_text END,processed_at=CASE WHEN attempts+1>=3 THEN now() ELSE processed_at END,next_attempt_at=$2,last_error='processing_failed' WHERE update_id=$1 AND status='pending'`, update.ID, time.Now().Add(delay))
	return err
}

func (repo Repository) RetryDelivery(ctx context.Context, update Update, retryAfter time.Duration, code string) error {
	delay := time.Duration(1<<min(update.SendAttempts+1, 6)) * time.Second
	if retryAfter > delay {
		delay = retryAfter
	}
	_, err := repo.DB.Exec(ctx, `UPDATE telegram_updates SET send_attempts=LEAST(send_attempts+1,1000000),delivery_next_attempt_at=$2,delivery_error=$3 WHERE update_id=$1 AND status IN ('pending','processed')`, update.ID, time.Now().Add(delay), code)
	return err
}

func (repo Repository) MarkSent(ctx context.Context, updateID int64) error {
	_, err := repo.DB.Exec(ctx, `UPDATE telegram_updates SET status='sent',sent_at=now(),delivery_error='' WHERE update_id=$1 AND status='processed'`, updateID)
	return err
}

func (repo Repository) Cleanup(ctx context.Context) error {
	if _, err := repo.DB.Exec(ctx, `DELETE FROM dialog_states WHERE expires_at<now()`); err != nil {
		return err
	}
	_, err := repo.DB.Exec(ctx, `DELETE FROM telegram_updates WHERE status='sent' AND sent_at<now()-interval '7 days'`)
	return err
}
