package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"familyexpenses/internal/expenses"
)

type apiRepo struct {
	expenses.Repository
	input expenses.Input
}

func (repo *apiRepo) Create(_ context.Context, input expenses.Input) (expenses.Expense, error) {
	repo.input = input
	return expenses.Expense{ID: 1, UserID: input.UserID, Amount: input.Amount}, nil
}
func (repo *apiRepo) Update(_ context.Context, actor, id int64, input expenses.Input) (expenses.Expense, error) {
	return expenses.Expense{}, expenses.ErrConflict
}
func (repo *apiRepo) Delete(_ context.Context, actor, id int64, version int) error {
	return expenses.ErrNotFound
}
func (repo *apiRepo) List(_ context.Context, filter expenses.Filter) (expenses.Page, error) {
	return expenses.Page{Items: []expenses.Expense{}, Total: 0}, nil
}

func TestPublicAPIValidationAndConflicts(t *testing.T) {
	repo := &apiRepo{}
	handler := (Server{Service: expenses.Service{Repo: repo}, Location: time.UTC, Ping: func(context.Context) error { return nil }}).Handler()
	for _, example := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/expenses", `{"user_id":"111","amount":"12,50","category":"еда","spent_at":"2026-10-16"}`, 201},
		{"POST", "/api/expenses", `{"user_id":"111","amount":"-5","category":"еда","spent_at":"2026-10-16"}`, 400},
		{"POST", "/api/expenses", `{"user_id":"111","amount":"12.00","category":"еда","spent_at":"2026-10-16","extra":true}`, 400},
		{"POST", "/api/expenses", `{} {}`, 400},
		{"PUT", "/api/expenses/1", `{"user_id":"111","amount":"10","category":"еда","spent_at":"2026-10-16","version":1}`, 409},
		{"DELETE", "/api/expenses/1?version=1", "", 404},
		{"DELETE", "/api/expenses/1", "", 400},
		{"GET", "/api/expenses?limit=huge", "", 400},
		{"GET", "/api/expenses?from=2026-02-30", "", 400},
		{"GET", "/api/expenses", "", 200},
	} {
		request := httptest.NewRequest(example.method, example.path, strings.NewReader(example.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != example.status {
			t.Errorf("%s %s: got %d want %d: %s", example.method, example.path, response.Code, example.status, response.Body.String())
		}
	}
	if repo.input.Amount != "12.50" || repo.input.UserID != 111 {
		t.Fatalf("bad persisted input %+v", repo.input)
	}
}

func TestReadinessReflectsDatabase(t *testing.T) {
	handler := (Server{Location: time.UTC, Ping: func(context.Context) error { return errors.New("offline") }}).Handler()
	for _, example := range []struct {
		path   string
		status int
	}{{"/health/live", 200}, {"/health/ready", 503}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, example.path, nil))
		if response.Code != example.status {
			t.Fatalf("%s: status %d", example.path, response.Code)
		}
	}
}
