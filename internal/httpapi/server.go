package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"familyexpenses/internal/expenses"
)

type Server struct {
	Service  expenses.Service
	Ping     func(context.Context) error
	Currency string
	Location *time.Location
	WebDir   string
}

func (server Server) Handler() http.Handler {
	mux := http.NewServeMux()
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
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "метод API не найден"})
	})
	mux.HandleFunc("GET /", server.static)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "same-origin")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'")
		if strings.HasPrefix(request.URL.Path, "/api/") {
			writer.Header().Set("Cache-Control", "no-store")
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("http panic", "path", request.URL.Path)
				writeJSON(writer, 500, map[string]string{"error": "внутренняя ошибка сервера"})
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
	page, err := server.Service.List(request.Context(), 0, filter)
	respond(writer, page, err)
}

func (server Server) summary(writer http.ResponseWriter, request *http.Request) {
	filter, err := parseFilter(request)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	summary, err := server.Service.Summary(request.Context(), 0, filter)
	respond(writer, summary, err)
}

func (server Server) create(writer http.ResponseWriter, request *http.Request) {
	var input expenses.Input
	if err := decode(writer, request, &input); err != nil {
		respond(writer, nil, err)
		return
	}
	expense, err := server.Service.Create(request.Context(), 0, input)
	if err != nil {
		respond(writer, nil, err)
		return
	}
	writeJSON(writer, http.StatusCreated, expense)
}

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
	expense, err := server.Service.Update(request.Context(), 0, id, input)
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
		respond(writer, nil, fmt.Errorf("%w: необходима версия расхода", expenses.ErrInvalid))
		return
	}
	if err := server.Service.Delete(request.Context(), 0, id, version); err != nil {
		respond(writer, nil, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func parseID(request *http.Request) (int64, error) {
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: неверный ID", expenses.ErrInvalid)
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
				return filter, fmt.Errorf("%w: неверный %s", expenses.ErrInvalid, name)
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
		return fmt.Errorf("%w: ожидается application/json", expenses.ErrInvalid)
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: неверный JSON", expenses.ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: лишние данные JSON", expenses.ErrInvalid)
	}
	return nil
}

func respond(writer http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(writer, http.StatusOK, value)
		return
	}
	status := http.StatusInternalServerError
	message := "внутренняя ошибка сервера"
	switch {
	case errors.Is(err, expenses.ErrInvalid):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, expenses.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, expenses.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	default:
		slog.Error("api operation failed", "error", err)
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
		http.Error(writer, "Frontend is not built. Run npm run build in web/.", http.StatusServiceUnavailable)
		return
	}
	if request.URL.Path == "/" {
		writer.Header().Set("Cache-Control", "no-cache")
	}
	http.FileServer(http.Dir(server.WebDir)).ServeHTTP(writer, request)
}
