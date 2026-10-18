package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"familyexpenses/internal/expenses"
	"github.com/jackc/pgx/v5"
)

func (repo Repository) createOnce(ctx context.Context, input expenses.Input) (expenses.Expense, error) {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	key := input.IdempotencyKey
	input.IdempotencyKey = ""
	data, err := json.Marshal(input)
	if err != nil {
		return expenses.Expense{}, err
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	source, ok := repo.DB.(beginner)
	if !ok {
		return expenses.Expense{}, errors.New("idempotent creation requires transactional database")
	}
	tx, err := source.Begin(ctx)
	if err != nil {
		return expenses.Expense{}, err
	}
	defer Rollback(tx)
	if err := LockLifecycle(ctx, tx); err != nil {
		return expenses.Expense{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 830124822))`, key); err != nil {
		return expenses.Expense{}, err
	}
	var oldHash string
	var response []byte
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT request_hash,response,revoked FROM expense_requests WHERE request_key=$1`, key).Scan(&oldHash, &response, &revoked)
	if err == nil {
		if revoked {
			return expenses.Expense{}, expenses.ErrGone
		}
		if oldHash != hash {
			return expenses.Expense{}, fmt.Errorf("%w: ключ запроса уже использован с другими данными", expenses.ErrConflict)
		}
		var result expenses.Expense
		err = json.Unmarshal(response, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return expenses.Expense{}, err
	}
	result, err := (Repository{DB: TimedDB{DB: tx}}).create(ctx, input)
	if err != nil {
		return result, err
	}
	response, err = json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO expense_requests(request_key,request_hash,response) VALUES($1,$2,$3)`, key, hash, response); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
