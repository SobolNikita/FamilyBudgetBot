package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"familyexpenses/internal/expenses"
)

type Server struct {
	Service        expenses.Service
	Ping           func(context.Context) error
	Currency       string
	Location       *time.Location
	WebDir         string
	RequestTimeout time.Duration
	MaxConcurrent  int
	BotStatus      func() (any, bool)
	QueueStatus    func(context.Context) (any, error)
}

func (server Server) Handler() http.Handler {
	limit := server.MaxConcurrent
	if limit <= 0 {
		limit = 32
	}
	admission := make(chan struct{}, limit)
	timeout := server.RequestTimeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/bot", func(w http.ResponseWriter, r *http.Request) {
		if server.BotStatus == nil {
			writeJSON(w, 200, map[string]string{"status": "disabled"})
			return
		}
		value, ready := server.BotStatus()
		status := 200
		if !ready {
			status = 503
		}
		writeJSON(w, status, value)
	})
	mux.HandleFunc("GET /health/queues", func(w http.ResponseWriter, r *http.Request) {
		if server.QueueStatus == nil {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		value, err := server.QueueStatus(ctx)
		respond(w, value, err)
	})
	mux.HandleFunc("GET /health/live", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := server.Ping(ctx); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/config", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"currency": server.Currency, "timezone": server.Location.String(), "today": time.Now().In(server.Location).Format(time.DateOnly)})
	})
	mux.HandleFunc("GET /api/users", func(writer http.ResponseWriter, request *http.Request) {
		users, err := server.Service.Repo.Users(request.Context())
		respond(writer, users, err)
	})
	mux.HandleFunc("GET /api/categories", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, expenses.Categories)
	})
	mux.HandleFunc("GET /api/expenses", server.list)
	mux.HandleFunc("POST /api/expenses", server.create)
	mux.HandleFunc("PUT /api/expenses/{id}", server.update)
	mux.HandleFunc("DELETE /api/expenses/{id}", server.delete)
	mux.HandleFunc("GET /api/reports/summary", server.summary)
	mux.HandleFunc("GET /api/", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "Эта возможность недоступна. Обновите страницу."})
	})
	mux.HandleFunc("GET /", server.static)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := rand.Text()
		writer.Header().Set("X-Request-ID", requestID)
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "same-origin")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'")
		if strings.HasPrefix(request.URL.Path, "/api/") || request.URL.Path == "/health/queues" {
			writer.Header().Set("Cache-Control", "no-store")
			select {
			case admission <- struct{}{}:
				defer func() { <-admission }()
			default:
				writer.Header().Set("Retry-After", "1")
				writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "Сервер занят. Попробуйте позже."})
				return
			}
			ctx, cancel := context.WithTimeout(request.Context(), timeout)
			defer cancel()
			request = request.WithContext(ctx)
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				// Panic values can contain request data or credentials; log only their type.
				slog.Error("http panic", "request_id", requestID, "method", request.Method, "path", request.URL.Path, "panic_type", fmt.Sprintf("%T", recovered), "stack", string(debug.Stack()))
				writeJSON(writer, 500, map[string]string{"error": "Не получилось выполнить действие. Попробуйте немного позже."})
			}
		}()
		mux.ServeHTTP(writer, request)
	})
}

func (server Server) list(writer http.ResponseWriter, request *http.Request) {
	filter, err := parseFilter(request)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	page, err := server.Service.List(request.Context(), expenses.FamilyActor, filter)
	respond(writer, page, err)
}

func (server Server) summary(writer http.ResponseWriter, request *http.Request) {
	filter, err := parseFilter(request)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	summary, err := server.Service.Summary(request.Context(), expenses.FamilyActor, filter)
	respond(writer, summary, err)
}

func (server Server) create(writer http.ResponseWriter, request *http.Request) {
	var input expenses.Input
	if err := decode(writer, request, &input); err != nil {
		respond(writer, nil, err)
		return
	}
	input.IdempotencyKey = request.Header.Get("Idempotency-Key")
	if !requestKeyPattern.MatchString(input.IdempotencyKey) {
		respond(writer, nil, fmt.Errorf("%w: обновите страницу и попробуйте сохранить расход ещё раз", expenses.ErrInvalid))
		return
	}
	expense, err := server.Service.Create(request.Context(), expenses.FamilyActor, input)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	writeJSON(writer, http.StatusCreated, expense)
}

var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func (server Server) update(writer http.ResponseWriter, request *http.Request) {
	id, err := parseID(request)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	var input expenses.Input
	if err := decode(writer, request, &input); err != nil {
		respond(writer, nil, err)
		return
	}
	expense, err := server.Service.Update(request.Context(), expenses.FamilyActor, id, input)
	respond(writer, expense, err)
}

func (server Server) delete(writer http.ResponseWriter, request *http.Request) {
	id, err := parseID(request)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	version, err := strconv.Atoi(request.URL.Query().Get("version"))
	if err != nil {
		respond(writer, nil, fmt.Errorf("%w: обновите список и выберите расход ещё раз", expenses.ErrInvalid))
		return
	}
	if err := server.Service.Delete(request.Context(), expenses.FamilyActor, id, version); err != nil {
		respond(writer, nil, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func parseID(request *http.Request) (int64, error) {
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: обновите список и выберите расход ещё раз", expenses.ErrInvalid)
	}
	return id, nil
}

func parseFilter(request *http.Request) (expenses.Filter, error) {
	query := request.URL.Query()
	filter := expenses.Filter{From: query.Get("from"), To: query.Get("to"), Category: query.Get("category"), Search: query.Get("search")}
	for name, target := range map[string]*int{"limit": &filter.Limit, "offset": &filter.Offset} {
		if raw := query.Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return filter, fmt.Errorf("%w: не удалось открыть выбранную страницу; обновите список расходов", expenses.ErrInvalid)
			}
			*target = value
		}
	}
	if raw := query.Get("user_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return filter, fmt.Errorf("%w: неверный участник", expenses.ErrInvalid)
		}
		filter.UserID = id
	}
	return filter, nil
}

func decode(writer http.ResponseWriter, request *http.Request, target any) error {
	if strings.Split(request.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fmt.Errorf("%w: не удалось прочитать данные; обновите страницу и попробуйте ещё раз", expenses.ErrInvalid)
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: не удалось прочитать данные; обновите страницу и попробуйте ещё раз", expenses.ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: не удалось прочитать данные; обновите страницу и попробуйте ещё раз", expenses.ErrInvalid)
	}
	return nil
}

func respond(writer http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(writer, http.StatusOK, value)
		return
	}
	status := http.StatusInternalServerError
	message := "Не получилось выполнить действие. Попробуйте немного позже."
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusGatewayTimeout, "Операция не завершилась вовремя. Обновите данные перед повтором."
	case errors.Is(err, context.Canceled):
		status, message = http.StatusRequestTimeout, "Запрос отменён."
	case errors.Is(err, expenses.ErrInvalid):
		status, message = http.StatusBadRequest, expenses.UserMessage(err)
	case errors.Is(err, expenses.ErrNotFound):
		status, message = http.StatusNotFound, expenses.UserMessage(err)
	case errors.Is(err, expenses.ErrConflict):
		status, message = http.StatusConflict, expenses.UserMessage(err)
	default:
		slog.Error("api operation failed", "request_id", writer.Header().Get("X-Request-ID"), "error", err)
	}
	writeJSON(writer, status, map[string]string{"error": message})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func (server Server) static(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" && !strings.HasPrefix(request.URL.Path, "/assets/") {
		http.NotFound(writer, request)
		return
	}
	if _, err := os.Stat(filepath.Join(server.WebDir, "index.html")); err != nil {
		http.Error(writer, "Панель временно недоступна. Попробуйте немного позже.", http.StatusServiceUnavailable)
		return
	}
	if request.URL.Path == "/" {
		writer.Header().Set("Cache-Control", "no-cache")
	}
	http.FileServer(http.Dir(server.WebDir)).ServeHTTP(writer, request)
}
