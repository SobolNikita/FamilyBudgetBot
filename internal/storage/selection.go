package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"familyexpenses/internal/expenses"
)

// SelectExpenses returns a stable fingerprint from one database snapshot.
// Pagination is deliberately excluded: selection covers the whole filter.
func (repo Repository) SelectExpenses(ctx context.Context, filter expenses.Filter) (expenses.Selection, error) {
	filter, err := expenses.ValidateFilter(filter)
	if err != nil {
		return expenses.Selection{}, err
	}
	filter.Limit, filter.Offset = 0, 0
	clause, args := where(filter)
	rows, err := repo.DB.Query(ctx, `SELECT id, version FROM expenses WHERE `+clause+` ORDER BY id`, args...)
	if err != nil {
		return expenses.Selection{}, err
	}
	defer rows.Close()
	hash := sha256.New()
	// Bind the fingerprint to the filter as well as the matching IDs/versions.
	encoded, err := json.Marshal(filter)
	if err != nil {
		return expenses.Selection{}, err
	}
	_, _ = hash.Write(encoded)
	selection := expenses.Selection{}
	for rows.Next() {
		var id int64
		var version int
		if err := rows.Scan(&id, &version); err != nil {
			return selection, err
		}
		_, _ = fmt.Fprintf(hash, "\n%d:%d", id, version)
		selection.Count++
	}
	if err := rows.Err(); err != nil {
		return selection, err
	}
	selection.Token = hex.EncodeToString(hash.Sum(nil))
	return selection, nil
}

// DeleteSelection either removes the exact confirmed set or changes nothing.
// The exclusive lifecycle lock prevents inserts, transfers, edits and member
// removal between checking the fingerprint and deleting the rows.
func (repo Repository) DeleteSelection(ctx context.Context, filter expenses.Filter, token string) (int64, error) {
	filter, err := expenses.ValidateFilter(filter)
	if err != nil {
		return 0, err
	}
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != sha256.Size {
		return 0, fmt.Errorf("%w: выберите расходы заново", expenses.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	source, ok := repo.DB.(beginner)
	if !ok {
		return 0, errors.New("bulk deletion requires transactional database")
	}
	tx, err := source.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer Rollback(tx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lifecycleLock); err != nil {
		return 0, err
	}
	current, err := (Repository{DB: TimedDB{DB: tx}}).SelectExpenses(ctx, filter)
	if err != nil {
		return 0, err
	}
	if current.Token != token || current.Count == 0 {
		return 0, expenses.ErrConflict
	}
	clause, args := where(filter)
	result, err := tx.Exec(ctx, `DELETE FROM expenses WHERE `+clause, args...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
