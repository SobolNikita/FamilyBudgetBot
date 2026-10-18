package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"familyexpenses/internal/expenses"
)

type panicRepo struct{ expenses.Repository }

func (panicRepo) Users(context.Context) ([]expenses.User, error) {
	panic("private panic value")
}

func TestPanicDiagnosticsIncludeStackAndServerRequestIDWithoutPrivateData(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	handler := Server{Service: expenses.Service{Repo: panicRepo{}}}.Handler()
	request := httptest.NewRequest("GET", "/api/users?secret=private-query", strings.NewReader("private-body"))
	request.Header.Set("X-Request-ID", "untrusted-id")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	id := response.Header().Get("X-Request-ID")
	if response.Code != 500 || id == "" || id == "untrusted-id" {
		t.Fatalf("bad recovery: %d %q", response.Code, id)
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["request_id"] != id || record["panic_type"] != "string" || record["method"] != "GET" || record["path"] != "/api/users" {
		t.Fatalf("missing diagnostic fields: %+v", record)
	}
	if stack, ok := record["stack"].(string); !ok || !strings.Contains(stack, "panicRepo.Users") {
		t.Fatal("panic origin missing from stack")
	}
	for _, secret := range []string{"private panic value", "private-query", "private-body", "untrusted-id"} {
		if strings.Contains(logs.String(), secret) || strings.Contains(response.Body.String(), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if strings.Contains(response.Body.String(), "stack") {
		t.Fatal("stack exposed to client")
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest("GET", "/health/live", nil))
	if second.Header().Get("X-Request-ID") == id {
		t.Fatal("request ID reused")
	}
}
