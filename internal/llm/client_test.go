package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolCallProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("invalid request path or authorization")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["tool_choice"] != "required" || body["parallel_tool_calls"] != false {
			t.Error("unbounded tool choice")
		}
		fmt.Fprint(writer, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expense","arguments":"{\"amount\":\"350\",\"category\":\"еда\",\"spent_at\":\"2026-10-16\"}"}}]}}]}`)
	}))
	defer server.Close()
	action, err := New(server.URL, "test-key", "test-model").Plan(context.Background(), Request{Text: "кофе 350", Today: "2026-10-16"})
	if err != nil || action.Name != "create_expense" || action.Amount == nil || *action.Amount != "350" {
		t.Fatalf("unexpected action %+v: %v", action, err)
	}
}

func TestRejectsUnsafeOrMalformedModelOutput(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"message":{"content":"Добавлено!"}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"execute_sql","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expense","arguments":"{\"user_id\":\"222\"}"}}]}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"delete_expense","arguments":"{}"}},{"function":{"name":"delete_expense","arguments":"{}"}}]}}]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { fmt.Fprint(writer, body) }))
		_, err := New(server.URL, "secret-key", "test").Plan(context.Background(), Request{})
		server.Close()
		if err == nil {
			t.Fatalf("accepted response %s", body)
		}
		if strings.Contains(err.Error(), "secret-key") {
			t.Fatal("error leaked API key")
		}
	}
}
