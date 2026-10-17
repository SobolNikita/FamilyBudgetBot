package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/config"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/httpapi"
	"familyexpenses/internal/llm"
	"familyexpenses/internal/storage"
	"familyexpenses/internal/telegram"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	poolConfig, err := pgxpool.ParseConfig(settings.DatabaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	poolConfig.MaxConns = 8
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("cannot initialize database pool")
	}
	pools := []*pgxpool.Pool{pool}
	defer func() {
		done := make(chan struct{})
		go func() {
			for _, p := range pools {
				p.Close()
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			slog.Error("database pool shutdown deadline exceeded")
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pool.Ping(startup); err != nil {
		return errors.New("database is unavailable")
	}
	repo := storage.Repository{DB: storage.TimedDB{DB: pool}}
	if err := repo.EnsureCurrency(startup, settings.Currency); err != nil {
		return err
	}
	api := httpapi.Server{Service: expenses.Service{Repo: repo}, Ping: pool.Ping, Currency: settings.Currency, Location: settings.Location, WebDir: settings.WebDir}
	api.QueueStatus = func(ctx context.Context) (any, error) {
		metrics, err := repo.Metrics(ctx)
		if err != nil {
			return nil, err
		}
		stats := pool.Stat()
		return map[string]any{"queue": metrics, "http_pool": map[string]any{"acquired": stats.AcquiredConns(), "idle": stats.IdleConns(), "max": stats.MaxConns(), "acquire_wait_ms_total": stats.AcquireDuration().Milliseconds(), "canceled_acquire_total": stats.CanceledAcquireCount()}}, nil
	}
	health := &telegram.Health{}
	if settings.BotEnabled {
		api.BotStatus = func() (any, bool) { value := health.Snapshot(); return value, value.Ready }
	}
	server := &http.Server{Addr: settings.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "address", settings.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()
	botErrors := make(chan error, 1)
	if settings.BotEnabled {
		botPool, err := pgxpool.NewWithConfig(ctx, poolConfig.Copy())
		if err != nil {
			stop()
			_ = server.Close()
			return errors.New("cannot initialize bot database pool")
		}
		pools = append(pools, botPool)
		planner := llm.New(settings.LLMBaseURL, settings.LLMKey, settings.LLMModel)
		planner.Timeout = settings.LLMTimeout
		runner := telegram.Runner{Pool: botPool, Client: telegram.New(settings.TelegramToken), Planner: &llm.Circuit{Planner: planner}, Engine: bot.Engine{Currency: settings.Currency}, Location: settings.Location, Health: health}
		go func() {
			for ctx.Err() == nil {
				err := runner.Run(ctx)
				if errors.Is(err, telegram.ErrShutdownTimeout) {
					botErrors <- err
					return
				}
				if err != nil && ctx.Err() == nil {
					health.Worker("runner_failed")
					slog.Error("bot stopped; restarting", "code", storage.ErrorCode(err))
				}
				select {
				case <-ctx.Done():
				case <-time.After(30 * time.Second):
				}
			}
			botErrors <- nil
		}()
	} else {
		slog.Info("Telegram bot disabled")
	}
	var failure error
	botFinished := false
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			failure = err
		}
	case err := <-botErrors:
		failure = err
		botFinished = true
	}
	stop()
	shutdown, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
	if settings.BotEnabled && !botFinished {
		select {
		case <-botErrors:
		case <-shutdown.Done():
			return errors.New("bot shutdown deadline exceeded")
		}
	}
	return failure
}
