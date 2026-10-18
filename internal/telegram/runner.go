package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/processing"
	"familyexpenses/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Runner struct {
	Pool     *pgxpool.Pool
	Client   *Client
	Planner  llm.Planner
	Engine   bot.Engine
	Location *time.Location
	Health   *Health
}

var ErrShutdownTimeout = errors.New("bot shutdown deadline exceeded")

func (runner Runner) Run(ctx context.Context) (result error) {
	startup, startupCancel := context.WithTimeout(ctx, storage.OperationTimeout)
	connection, err := runner.Pool.Acquire(startup)
	startupCancel()
	if err != nil {
		return err
	}
	defer connection.Release()
	var locked bool
	if err := (storage.TimedDB{DB: connection}).QueryRow(ctx, `SELECT pg_try_advisory_lock(830124820)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("another Telegram worker is already running")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = connection.Exec(cleanup, `SELECT pg_advisory_unlock(830124820)`)
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	runner.Health.Worker("")
	defer runner.Health.Worker("stopped")
	var group sync.WaitGroup
	start := func(work func()) { group.Add(1); go func() { defer group.Done(); work() }() }
	start(func() { runner.poll(ctx) })
	start(func() { runner.deliver(ctx) })
	repo := runner.repository()
	start(func() {
		for ctx.Err() == nil {
			if err := repo.Cleanup(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("queue cleanup failed")
			}
			if !pause(ctx, time.Minute) {
				return
			}
		}
	})
	lockLost := make(chan struct{})
	start(func() {
		for pause(ctx, time.Second) {
			var alive int
			if err := (storage.TimedDB{DB: connection}).QueryRow(ctx, `SELECT 1`).Scan(&alive); err != nil {
				close(lockLost)
				cancel()
				return
			}
		}
	})
	defer func() {
		cancel()
		done := make(chan struct{})
		go func() { group.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			slog.Error("bot shutdown deadline exceeded")
			result = ErrShutdownTimeout
		}
	}()
	active := map[int64]bool{}
	completed := make(chan int64, 4)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		for len(active) < 4 && ctx.Err() == nil {
			excluded := make([]int64, 0, len(active))
			for userID := range active {
				excluded = append(excluded, userID)
			}
			update, err := repo.NextUpdate(ctx, excluded...)
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("queue read failed")
				}
				break
			}
			if err := repo.TouchProcessing(ctx, update.UserID); err != nil {
				break
			}
			active[update.UserID] = true
			start(func() {
				operation, cancel := context.WithTimeout(ctx, 150*time.Second)
				err := runner.process(operation, repo, update)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Warn("bot processing failed", "update_id", update.ID, "code", storage.ErrorCode(err))
					if err := repo.RetryProcessing(ctx, update); err != nil {
						slog.Warn("queue retry scheduling failed")
					}
				}
				completed <- update.UserID
			})
		}
		select {
		case userID := <-completed:
			delete(active, userID)
		case <-ticker.C:
		case <-ctx.Done():
		}
	}
	select {
	case <-lockLost:
		return errors.New("Telegram worker lock connection lost")
	default:
		return nil
	}
}

func (runner Runner) repository() storage.Repository {
	return storage.Repository{DB: storage.TimedDB{DB: runner.Pool}}
}

func (runner Runner) poll(ctx context.Context) {
	var offset int64
	repo := runner.repository()
	failures := 0
	for ctx.Err() == nil {
		updates, err := runner.Client.Poll(ctx, offset)
		if err != nil {
			failures++
			delay, code := pollRetry(err, failures)
			runner.Health.poll(code)
			if ctx.Err() == nil {
				slog.Warn("Telegram polling failed", "error", err)
			}
			if !pause(ctx, delay) {
				return
			}
			continue
		}
		failures = 0
		runner.Health.poll("")
		for _, update := range updates {
			message := update.Message
			if message != nil && message.Chat.Type == "private" && message.From.ID > 0 && message.Chat.ID == message.From.ID {
				text := strings.TrimSpace(message.Text)
				if text == "" {
					text = "[неподдерживаемый тип сообщения]"
				}
				var messageAt time.Time
				if message.Date > 0 {
					messageAt = time.Unix(message.Date, 0)
				}
				if err := repo.Enqueue(ctx, storage.Update{ID: update.ID, UserID: message.From.ID, ChatID: message.Chat.ID, MessageID: message.ID, Text: text, MessageAt: messageAt}); err != nil {
					runner.Health.poll("persistence_failed")
					slog.Error("Telegram update persistence failed")
					pause(ctx, 3*time.Second)
					break
				}
			}
			offset = update.ID + 1
		}
	}
}

func (runner Runner) process(ctx context.Context, repo storage.Repository, update storage.Update) error {
	return (processing.Processor{Pool: runner.Pool, Planner: runner.Planner, Engine: runner.Engine, Location: runner.Location, ReportLLM: runner.Health.llm}).Process(ctx, repo, update)
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
