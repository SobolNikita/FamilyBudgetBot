package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func ErrorCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return "sql_" + pg.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "processing_failed"
}

type QueueMetrics struct {
	Pending              int     `json:"pending"`
	Results              int     `json:"results"`
	Suspended            int     `json:"suspended"`
	OldestPendingSeconds float64 `json:"oldest_pending_seconds"`
	OldestResultSeconds  float64 `json:"oldest_result_seconds"`
}

func (repo Repository) Metrics(ctx context.Context) (QueueMetrics, error) {
	var m QueueMetrics
	err := repo.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='processed'),count(*) FILTER(WHERE delivery_suspended),COALESCE(EXTRACT(EPOCH FROM now()-min(created_at) FILTER(WHERE status='pending')),0)::float8,COALESCE(EXTRACT(EPOCH FROM now()-min(processed_at) FILTER(WHERE status='processed')),0)::float8 FROM telegram_updates WHERE status IN ('pending','processed')`).Scan(&m.Pending, &m.Results, &m.Suspended, &m.OldestPendingSeconds, &m.OldestResultSeconds)
	return m, err
}
