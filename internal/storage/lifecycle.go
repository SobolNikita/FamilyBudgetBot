package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const OperationTimeout = 10 * time.Second

// TimedDB bounds pool acquisition and the complete consumption of query results.
type TimedDB struct{ DB DB }

func (db TimedDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	return db.DB.Exec(ctx, sql, args...)
}
func (db TimedDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	return timedRow{Row: db.DB.QueryRow(ctx, sql, args...), cancel: cancel}
}

type timedRow struct {
	pgx.Row
	cancel context.CancelFunc
}

func (row timedRow) Scan(args ...any) error { defer row.cancel(); return row.Row.Scan(args...) }
func (db TimedDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	rows, err := db.DB.Query(ctx, sql, args...)
	if err != nil {
		cancel()
		return nil, err
	}
	return &timedRows{Rows: rows, cancel: cancel}, nil
}

type timedRows struct {
	pgx.Rows
	cancel context.CancelFunc
}

func (rows *timedRows) Close() { rows.Rows.Close(); rows.cancel() }
func (rows *timedRows) Next() bool {
	ok := rows.Rows.Next()
	if !ok {
		rows.Close()
	}
	return ok
}

type beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

func (db TimedDB) Begin(ctx context.Context) (pgx.Tx, error) {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	source, ok := db.DB.(beginner)
	if !ok {
		return nil, errors.New("database does not support transactions")
	}
	return source.Begin(ctx)
}
func Rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (repo Repository) EnsureCurrency(ctx context.Context, currency string) error {
	_, err := repo.DB.Exec(ctx, `INSERT INTO budget_settings(currency) VALUES($1) ON CONFLICT DO NOTHING`, currency)
	if err != nil {
		return err
	}
	var stored string
	if err := repo.DB.QueryRow(ctx, `SELECT currency FROM budget_settings WHERE singleton`).Scan(&stored); err != nil {
		return err
	}
	if stored != currency {
		return errors.New("FAMILY_CURRENCY differs from persisted budget currency; explicit data migration required")
	}
	return nil
}
