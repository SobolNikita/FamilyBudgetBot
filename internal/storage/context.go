package storage

import (
	"context"
	"familyexpenses/internal/dialog"
	"familyexpenses/internal/expenses"
)

// RefreshContext treats saved expenses as references, not an authoritative copy.
// Pending confirmations deliberately keep their original version.
func (repo Repository) RefreshContext(ctx context.Context, userID int64, state dialog.State) (dialog.State, error) {
	ids := make([]int64, 0, len(state.Context.Shown)+1)
	for _, item := range state.Context.Shown {
		ids = append(ids, item.ID)
	}
	if state.Context.LastExpense != nil {
		ids = append(ids, state.Context.LastExpense.ID)
	}
	if len(ids) == 0 {
		return state, nil
	}
	rows, err := repo.DB.Query(ctx, `SELECT `+columns+` FROM expenses WHERE user_id=$1 AND id=ANY($2::bigint[])`, userID, ids)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	current := make(map[int64]expenses.Expense)
	for rows.Next() {
		item, err := scanExpense(rows)
		if err != nil {
			return state, err
		}
		current[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return state, err
	}
	changed := false
	shown := make([]expenses.Expense, 0, len(state.Context.Shown))
	for _, old := range state.Context.Shown {
		fresh, ok := current[old.ID]
		if !ok {
			changed = true
			continue
		}
		if fresh.Version != old.Version || fresh.UserID != old.UserID {
			changed = true
		}
		shown = append(shown, fresh)
	}
	state.Context.Shown = shown
	if old := state.Context.LastExpense; old != nil {
		fresh, ok := current[old.ID]
		if !ok {
			state.Context.LastExpense = nil
			changed = true
		} else {
			if fresh.Version != old.Version || fresh.UserID != old.UserID {
				changed = true
			}
			state.Context.LastExpense = &fresh
		}
	}
	// Historical replies may quote the removed values. Don't send contradictory
	// facts back to the model as if they were still current.
	if changed {
		state.Context.Turns = nil
	}
	return state, nil
}
