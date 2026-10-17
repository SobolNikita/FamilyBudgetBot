package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestSubjectIsRequiredOnEveryTool(t *testing.T) {
	for _, tool := range toolDefinitions() {
		function := tool["function"].(map[string]any)
		parameters := function["parameters"].(map[string]any)
		if !slices.Contains(parameters["required"].([]string), "subject") {
			t.Fatalf("subject optional for %s", function["name"])
		}
		properties := parameters["properties"].(map[string]any)
		if properties["subject"] == nil {
			t.Fatal("subject schema missing")
		}
	}
}

func TestCurrentUserAndSubjectProtocol(t *testing.T) {
	for _, subject := range []string{"self", "other", "family", "unclear", "", "admin"} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var body struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			var input Request
			if len(body.Messages) != 2 {
				t.Error("unexpected messages")
				return
			}
			if err := json.Unmarshal([]byte(body.Messages[1].Content), &input); err != nil {
				t.Error(err)
			}
			if input.CurrentUser != "Никита" || input.Text != "Сколько потратила Ева за неделю" {
				t.Error("sender identity missing or changed")
			}
			arguments := `{}`
			if subject != "" {
				arguments = fmt.Sprintf(`{"subject":%q}`, subject)
			}
			encoded, _ := json.Marshal(arguments)
			fmt.Fprintf(writer, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"summarize_expenses","arguments":%s}}]}}]}`, encoded)
		}))
		action, err := New(server.URL, "test-key", "test").Plan(context.Background(), Request{CurrentUser: "Никита", Text: "Сколько потратила Ева за неделю"})
		server.Close()
		if subject == "" || subject == "admin" {
			if err == nil {
				t.Fatal("invalid or omitted owner accepted")
			}
		} else if err != nil || action.Subject != subject {
			t.Fatalf("subject lost: %+v %v", action, err)
		}
	}
}
