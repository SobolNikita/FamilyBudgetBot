package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

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
func (repo Repository) DeleteSelection(ctx context.Context, filter expenses.Filter, input expenses.DeleteSelectionRequest) (int64, error) {
	filter, err := expenses.ValidateFilter(filter)
	if err != nil {
		return 0, err
	}
	ids, versions, err := validateSelection(input)
	if err != nil {
		return 0, err
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
	clause, args := where(filter)
	expected := len(ids)
	if input.Token != "" {
		current, err := (Repository{DB: TimedDB{DB: tx}}).SelectExpenses(ctx, filter)
		if err != nil {
			return 0, err
		}
		if current.Token != input.Token || current.Count <= len(ids) {
			return 0, expenses.ErrConflict
		}
		// Every excluded ID must belong to the confirmed set; invalid exclusions
		// must never turn into a broader deletion than the user intended.
		args = append(args, ids)
		var excluded int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE `+clause+fmt.Sprintf(` AND id=ANY($%d::bigint[])`, len(args)), args...).Scan(&excluded); err != nil {
			return 0, err
		}
		if excluded != len(ids) {
			return 0, expenses.ErrConflict
		}
		clause += fmt.Sprintf(` AND NOT(id=ANY($%d::bigint[]))`, len(args))
		expected = current.Count - excluded
	} else {
		args = append(args, ids, versions)
		clause += fmt.Sprintf(` AND (id,version) IN (SELECT * FROM unnest($%d::bigint[], $%d::integer[]))`, len(args)-1, len(args))
		var matching int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE `+clause, args...).Scan(&matching); err != nil {
			return 0, err
		}
		if matching != expected {
			return 0, expenses.ErrConflict
		}
	}
	result, err := tx.Exec(ctx, `DELETE FROM expenses WHERE `+clause, args...)
	if err != nil {
		return 0, err
	}
	if result.RowsAffected() != int64(expected) {
		return 0, expenses.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func validateSelection(input expenses.DeleteSelectionRequest) ([]int64, []int, error) {
	invalid := fmt.Errorf("%w: выберите расходы заново; максимум %d отдельных записей или исключений", expenses.ErrInvalid, expenses.MaxSelectionItems)
	ids := make([]int64, 0, len(input.Items)+len(input.Exclude))
	versions := make([]int, 0, len(input.Items))
	if input.Token != "" {
		decoded, err := hex.DecodeString(input.Token)
		if err != nil || len(decoded) != sha256.Size || len(input.Items) != 0 || len(input.Exclude) > expenses.MaxSelectionItems {
			return nil, nil, invalid
		}
		for _, raw := range input.Exclude {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				return nil, nil, invalid
			}
			ids = append(ids, id)
		}
	} else {
		if len(input.Exclude) != 0 || len(input.Items) == 0 || len(input.Items) > expenses.MaxSelectionItems {
			return nil, nil, invalid
		}
		for _, item := range input.Items {
			if item.ID <= 0 || item.Version <= 0 || int64(item.Version) > 2147483647 {
				return nil, nil, invalid
			}
			ids = append(ids, item.ID)
			versions = append(versions, item.Version)
		}
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, nil, invalid
		}
		seen[id] = true
	}
	return ids, versions, nil
}
