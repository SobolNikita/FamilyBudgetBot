package storage

import (
	"context"
	"errors"
	"familyexpenses/internal/expenses"
	"fmt"
	"github.com/jackc/pgx/v5"
)

const RemovedMemberReply = "Вы удалены из семьи. Ваши расходы и история диалога удалены. Для возвращения попросите восстановить доступ в веб-панели; затем представьтесь заново."

// All application writes and Telegram sends take the shared transaction lock.
// Removal/restoration take it exclusively, serializing the lifecycle boundary
// without keeping a database transaction open during an LLM call.
const lifecycleLock int64 = 830124823

func LockLifecycle(ctx context.Context, db DB) error {
	_, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, lifecycleLock)
	return err
}

func (repo Repository) Write(ctx context.Context, action func(Repository) error) error {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	source, ok := repo.DB.(beginner)
	if !ok {
		return errors.New("operation requires transactional database")
	}
	tx, err := source.Begin(ctx)
	if err != nil {
		return err
	}
	defer Rollback(tx)
	if err := LockLifecycle(ctx, tx); err != nil {
		return err
	}
	if err := action(Repository{DB: TimedDB{DB: tx}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repo Repository) RemovedUsers(ctx context.Context) ([]expenses.User, error) {
	rows, err := repo.DB.Query(ctx, `SELECT id, membership_version, removed_at FROM users WHERE removed_at IS NOT NULL ORDER BY removed_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []expenses.User{}
	for rows.Next() {
		var user expenses.User
		if err := rows.Scan(&user.ID, &user.Version, &user.RemovedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (repo Repository) RemoveUser(ctx context.Context, id int64, version int) error {
	return repo.changeMembership(ctx, id, version, false)
}
func (repo Repository) RestoreUser(ctx context.Context, id int64, version int) error {
	return repo.changeMembership(ctx, id, version, true)
}

func (repo Repository) changeMembership(ctx context.Context, id int64, version int, restore bool) error {
	if id <= 0 || version < 1 {
		return fmt.Errorf("%w: обновите список участников", expenses.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	source, ok := repo.DB.(beginner)
	if !ok {
		return errors.New("member changes require transactional database")
	}
	tx, err := source.Begin(ctx)
	if err != nil {
		return err
	}
	defer Rollback(tx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lifecycleLock); err != nil {
		return err
	}
	var current int
	var removed bool
	if err := tx.QueryRow(ctx, `SELECT membership_version,removed_at IS NOT NULL FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&current, &removed); err != nil {
		return translate(err)
	}
	// A repeat of the completed request is harmless. An old request must not
	// remove a participant who has since been restored (or restore a new removal).
	if current == version+1 && removed != restore {
		return nil
	}
	if current != version || removed != restore {
		return expenses.ErrConflict
	}
	if restore {
		_, err = tx.Exec(ctx, `UPDATE telegram_updates SET payload='{}',reply_text='',status='sent',sent_at=now(),reply_message_id=0,last_error='membership_restored' WHERE user_id=$1 AND last_error='member_removed' AND status IN ('pending','processed')`, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET removed_at=NULL,membership_version=membership_version+1,updated_at=now(),delivery_paused=false WHERE id=$1`, id)
	} else {
		// Revoke snapshots before deleting their expenses. Preserve only the key and
		// hash: retrying an old creation can never resurrect deleted personal data.
		_, err = tx.Exec(ctx, `UPDATE expense_requests SET response='{}',revoked=true WHERE response->>'user_id'=$1::bigint::text OR response->>'id' IN (SELECT id::text FROM expenses WHERE user_id=$1)`, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM dialog_states WHERE user_id=$1
   OR context->'last_expense'->>'user_id'=$1::bigint::text
   OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(context->'shown')='array' THEN context->'shown' ELSE '[]'::jsonb END) item WHERE item->>'user_id'=$1::bigint::text OR item->>'id' IN (SELECT id::text FROM expenses WHERE user_id=$1))
   OR pending_action->>'id' IN (SELECT id::text FROM expenses WHERE user_id=$1)`, id)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM expenses WHERE user_id=$1`, id); err != nil {
			return err
		}
		// Keep update IDs for transport deduplication, but purge message bodies and
		// cached replies, including work that was waiting on the LLM or delivery.
		_, err = tx.Exec(ctx, `UPDATE telegram_updates SET payload='{}',reply_text='',status='sent',sent_at=now(),last_error='member_removed',delivery_error='',delivery_suspended=false,reply_message_id=0,message_id=0 WHERE user_id=$1`, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET display_name='',pending_name='',name_requested_at=NULL,removed_at=now(),membership_version=membership_version+1,updated_at=now(),delivery_paused=false WHERE id=$1`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Refresh the delivery after taking the lifecycle lock: it may have been purged
// since the scheduler read it. The lock stays held until the send is finished.
func (repo Repository) DeliverCurrent(ctx context.Context, id int64, deliver func(Repository, Update) error) error {
	return repo.Write(ctx, func(current Repository) error {
		update, err := scanUpdate(current.DB.QueryRow(ctx, `SELECT `+updateColumns+` FROM telegram_updates WHERE update_id=$1 AND status IN ('pending','processed')`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return deliver(current, update)
	})
}
