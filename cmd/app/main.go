package main

import (
	"context"
	"errors"
	"log/slog"
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
	defer pool.Close()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pool.Ping(startup); err != nil {
		return errors.New("database is unavailable")
	}
	repo := storage.Repository{DB: pool}
	api := httpapi.Server{Service: expenses.Service{Repo: repo}, Ping: pool.Ping, Currency: settings.Currency, Location: settings.Location, WebDir: settings.WebDir}
	server := &http.Server{Addr: settings.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "address", settings.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()
	botErrors := make(chan error, 1)
	if settings.BotEnabled {
		runner := telegram.Runner{Pool: pool, Client: telegram.New(settings.TelegramToken), Planner: llm.New(settings.LLMBaseURL, settings.LLMKey, settings.LLMModel), Engine: bot.Engine{Currency: settings.Currency}, Location: settings.Location}
		go func() { botErrors <- runner.Run(ctx) }()
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
	shutdown, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
	if settings.BotEnabled && !botFinished {
		<-botErrors
	}
	return failure
}
