package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"familyexpenses/internal/expenses"
)

type waitingRepo struct {
	expenses.Repository
	entered chan struct{}
}

func (repo waitingRepo) Users(ctx context.Context) ([]expenses.User, error) {
	repo.entered <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestRequestsHaveDeadlineAndOverloadIsRejected(t *testing.T) {
	entered := make(chan struct{}, 1)
	handler := Server{Service: expenses.Service{Repo: waitingRepo{entered: entered}}, RequestTimeout: 80 * time.Millisecond, MaxConcurrent: 1}.Handler()
	finished := make(chan int, 1)
	go func() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/users", nil))
		finished <- response.Code
	}()
	<-entered
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/users", nil))
	if response.Code != 503 {
		t.Fatalf("overload status=%d", response.Code)
	}
	select {
	case code := <-finished:
		if code != 504 {
			t.Fatalf("timeout status=%d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("handler leaked after deadline")
	}
}
func TestBotHealthIsIndependentOfDatabase(t *testing.T) {
	handler := Server{Ping: func(context.Context) error { return nil }, BotStatus: func() (any, bool) { return map[string]string{"code": "telegram_http_401"}, false }}.Handler()
	for _, item := range []struct {
		path   string
		status int
	}{{"/health/ready", 200}, {"/health/bot", 503}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", item.path, nil))
		if response.Code != item.status {
			t.Fatalf("%s: %d", item.path, response.Code)
		}
	}
}
