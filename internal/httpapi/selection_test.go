package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"familyexpenses/internal/expenses"
)

type selectionStub struct {
	filter expenses.Filter
	token  string
	err    error
	writes int
}

func (s *selectionStub) SelectExpenses(_ context.Context, f expenses.Filter) (expenses.Selection, error) {
	s.filter = f
	return expenses.Selection{Token: "snapshot", Count: 45}, s.err
}
func (s *selectionStub) DeleteSelection(_ context.Context, f expenses.Filter, token string) (int64, error) {
	s.filter, s.token = f, token
	s.writes++
	return 45, s.err
}

func TestSelectionRoutes(t *testing.T) {
	repo := &selectionStub{}
	handler := (Server{Selections: repo}).Handler()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/api/expenses/selection?user_id=111&from=2026-10-18&to=2026-10-18&search=coffee&limit=20&offset=20"
	if w := request("GET", path, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"count":45`) {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	if repo.filter.UserID != 111 || repo.filter.Search != "coffee" || repo.filter.From != "2026-10-18" {
		t.Fatalf("filter lost: %+v", repo.filter)
	}
	if w := request("POST", "/api/expenses/bulk-delete?user_id=111", `{"token":"snapshot"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":45`) {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if repo.token != "snapshot" || repo.filter.UserID != 111 {
		t.Fatal("precondition/filter lost")
	}
	repo.err = expenses.ErrConflict
	if w := request("POST", "/api/expenses/bulk-delete", `{"token":"stale"}`); w.Code != http.StatusConflict {
		t.Fatalf("conflict: %d", w.Code)
	}
	writes := repo.writes
	for _, path := range []string{"/api/expenses/bulk-delete?user_id=invalid", "/api/expenses/bulk-delete?from=invalid", "/api/expenses/bulk-delete?category=invalid"} {
		if w := request("POST", path, `{"token":"snapshot"}`); w.Code != 400 {
			t.Fatalf("validation: %d", w.Code)
		}
	}
	if w := request("POST", "/api/expenses/bulk-delete", `{"token":"snapshot","unknown":1}`); w.Code != 400 {
		t.Fatalf("body validation: %d", w.Code)
	}
	if repo.writes != writes {
		t.Fatal("invalid request reached storage")
	}
}
