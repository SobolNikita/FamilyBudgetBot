package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"familyexpenses/internal/expenses"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{ DB DB }

const columns = `id, user_id, amount::text, category, to_char(spent_at, 'YYYY-MM-DD'), comment, version, created_at, updated_at`

func scanExpense(row pgx.Row) (expenses.Expense, error) {
	var expense expenses.Expense
	err := row.Scan(&expense.ID, &expense.UserID, &expense.Amount, &expense.Category, &expense.SpentAt, &expense.Comment, &expense.Version, &expense.CreatedAt, &expense.UpdatedAt)
	return expense, translate(err)
}

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return expenses.ErrNotFound
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && (pgError.Code == "23503" || pgError.Code == "23514" || pgError.Code == "22003") {
		return fmt.Errorf("%w: проверьте участника и параметры расхода", expenses.ErrInvalid)
	}
	return err
}

func (repo Repository) Users(ctx context.Context) ([]expenses.User, error) {
	rows, err := repo.DB.Query(ctx, `SELECT id, display_name FROM users WHERE display_name <> '' ORDER BY display_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []expenses.User{}
	for rows.Next() {
		var user expenses.User
		if err := rows.Scan(&user.ID, &user.DisplayName); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func where(filter expenses.Filter) (string, []any) {
	clauses := []string{"TRUE"}
	args := []any{}
	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expression, len(args)))
	}
	if filter.UserID != 0 {
		add("user_id = $%d", filter.UserID)
	}
	if filter.From != "" {
		add("spent_at >= $%d::date", filter.From)
	}
	if filter.To != "" {
		add("spent_at <= $%d::date", filter.To)
	}
	if filter.Category != "" {
		add("category = $%d", filter.Category)
	}
	if filter.Search != "" {
		add("strpos(lower(comment), lower($%d)) > 0", filter.Search)
	}
	return strings.Join(clauses, " AND "), args
}

func (repo Repository) List(ctx context.Context, filter expenses.Filter) (expenses.Page, error) {
	clause, args := where(filter)
	page := expenses.Page{Items: []expenses.Expense{}}
	if err := repo.DB.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE `+clause, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	query := fmt.Sprintf(`SELECT %s FROM expenses WHERE %s ORDER BY spent_at DESC, id DESC LIMIT $%d OFFSET $%d`, columns, clause, len(args)+1, len(args)+2)
	args = append(args, filter.Limit, filter.Offset)
	rows, err := repo.DB.Query(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		expense, err := scanExpense(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, expense)
	}
	return page, rows.Err()
}

func (repo Repository) Get(ctx context.Context, actor, id int64) (expenses.Expense, error) {
	return scanExpense(repo.DB.QueryRow(ctx, `SELECT `+columns+` FROM expenses WHERE id=$1 AND ($2::bigint=0 OR user_id=$2)`, id, actor))
}

func (repo Repository) Create(ctx context.Context, input expenses.Input) (expenses.Expense, error) {
	if err := repo.requireRegisteredUser(ctx, input.UserID); err != nil {
		return expenses.Expense{}, err
	}
	return scanExpense(repo.DB.QueryRow(ctx, `INSERT INTO expenses (user_id, amount, category, spent_at, comment) VALUES ($1, $2::numeric, $3, $4::date, $5) RETURNING `+columns, input.UserID, input.Amount, input.Category, input.SpentAt, input.Comment))
}

func (repo Repository) Update(ctx context.Context, actor, id int64, input expenses.Input) (expenses.Expense, error) {
	if err := repo.requireRegisteredUser(ctx, input.UserID); err != nil {
		return expenses.Expense{}, err
	}
	expense, err := scanExpense(repo.DB.QueryRow(ctx, `UPDATE expenses SET user_id=$3, amount=$4::numeric, category=$5, spent_at=$6::date, comment=$7, version=version+1, updated_at=now() WHERE id=$1 AND ($2::bigint=0 OR user_id=$2) AND version=$8 RETURNING `+columns, id, actor, input.UserID, input.Amount, input.Category, input.SpentAt, input.Comment, input.Version))
	if errors.Is(err, expenses.ErrNotFound) {
		return expense, repo.missingOrConflict(ctx, actor, id)
	}
	return expense, err
}

func (repo Repository) requireRegisteredUser(ctx context.Context, userID int64) error {
	var registered bool
	if err := repo.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND display_name <> '')`, userID).Scan(&registered); err != nil {
		return err
	}
	if !registered {
		return fmt.Errorf("%w: участник должен сначала представиться боту", expenses.ErrInvalid)
	}
	return nil
}

func (repo Repository) Delete(ctx context.Context, actor, id int64, version int) error {
	result, err := repo.DB.Exec(ctx, `DELETE FROM expenses WHERE id=$1 AND ($2::bigint=0 OR user_id=$2) AND version=$3`, id, actor, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return repo.missingOrConflict(ctx, actor, id)
	}
	return nil
}

func (repo Repository) missingOrConflict(ctx context.Context, actor, id int64) error {
	if _, err := repo.Get(ctx, actor, id); err != nil {
		return err
	}
	return expenses.ErrConflict
}

func (repo Repository) Summary(ctx context.Context, filter expenses.Filter) (expenses.Summary, error) {
	clause, args := where(filter)
	rows, err := repo.DB.Query(ctx, `SELECT COALESCE(category, ''), COALESCE(sum(amount), 0)::text, count(*) FROM expenses WHERE `+clause+` GROUP BY GROUPING SETS ((category), ()) ORDER BY category NULLS FIRST`, args...)
	if err != nil {
		return expenses.Summary{}, err
	}
	defer rows.Close()
	summary := expenses.Summary{Amount: "0.00", Categories: []expenses.CategoryTotal{}}
	for rows.Next() {
		var item expenses.CategoryTotal
		if err := rows.Scan(&item.Category, &item.Amount, &item.Count); err != nil {
			return summary, err
		}
		if item.Category == "" {
			summary.Amount, summary.Count = item.Amount, item.Count
		} else {
			summary.Categories = append(summary.Categories, item)
		}
	}
	return summary, rows.Err()
}
