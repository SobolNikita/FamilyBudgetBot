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
		fmt.Fprint(writer, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expenses","arguments":"{\"expenses\":[{\"amount\":\"350\",\"category\":\"еда\",\"spent_at\":\"2026-10-16\",\"comment\":\"кофе\"}]}"}}]}}]}`)
	}))
	defer server.Close()
	action, err := New(server.URL, "test-key", "test-model").Plan(context.Background(), Request{Text: "кофе 350", Today: "2026-10-16"})
	if err != nil || action.Name != "create_expenses" || len(action.Expenses) != 1 || action.Expenses[0].Amount != "350" {
		t.Fatalf("unexpected action %+v: %v", action, err)
	}
}

func TestBatchToolSchemaAndDecoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name       string `json:"name"`
					Parameters struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		found := false
		for _, tool := range body.Tools {
			if tool.Function.Name == "create_expense" {
				t.Error("single-item tool can silently discard remaining purchases")
			}
			if tool.Function.Name == "create_expenses" {
				found = true
				var schema struct {
					Type               string
					MinItems, MaxItems int
					Items              struct {
						AdditionalProperties bool
						Required             []string
					}
				}
				if err := json.Unmarshal(tool.Function.Parameters.Properties["expenses"], &schema); err != nil {
					t.Error(err)
				}
				if schema.Type != "array" || schema.MinItems != 1 || schema.MaxItems != MaxExpensesPerMessage || schema.Items.AdditionalProperties || len(schema.Items.Required) != 4 {
					t.Errorf("invalid batch schema: %+v", schema)
				}
			}
		}
		if !found {
			t.Error("batch creation tool missing")
		}
		fmt.Fprint(writer, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expenses","arguments":"{\"expenses\":[{\"amount\":\"5\",\"category\":\"еда\",\"spent_at\":\"2026-10-17\",\"comment\":\"Чипсы\"},{\"amount\":\"8\",\"category\":\"еда\",\"spent_at\":\"2026-10-17\",\"comment\":\"Йогурт\"}]}"}}]}}]}`)
	}))
	defer server.Close()
	action, err := New(server.URL, "test-key", "test").Plan(context.Background(), Request{Text: "Чипсы 5 Йогурт 8"})
	if err != nil || len(action.Expenses) != 2 || action.Expenses[0].Comment != "Чипсы" || action.Expenses[1].Amount != "8" {
		t.Fatalf("batch lost items: %+v %v", action, err)
	}
}

func TestRejectsUnsafeOrMalformedModelOutput(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"message":{"content":"Добавлено!"}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"execute_sql","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expense","arguments":"{\"user_id\":\"222\"}"}}]}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expenses","arguments":"{\"expenses\":[{\"amount\":\"5\",\"user_id\":\"222\"}]}"}}]}}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"name":"create_expenses","arguments":"{\"expenses\":{\"amount\":\"5\"}}"}}]}}]}`,
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
