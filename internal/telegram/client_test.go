package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPollingAndPlainTextReplies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch request.URL.Path {
		case "/getUpdates":
			if body["offset"] != float64(42) {
				t.Error("polling offset not passed")
			}
			fmt.Fprint(writer, `{"ok":true,"result":[{"update_id":42,"message":{"text":"кофе 350","from":{"id":111},"chat":{"id":111,"type":"private"}}}]}`)
		case "/sendMessage":
			if body["text"] != "<b>обычный текст</b>" || body["parse_mode"] != nil {
				t.Error("reply text should not be interpreted as markup")
			}
			fmt.Fprint(writer, `{"ok":true,"result":{"message_id":1}}`)
		default:
			t.Error("unexpected Telegram method")
		}
	}))
	defer server.Close()
	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	updates, err := client.Poll(context.Background(), 42)
	if err != nil || len(updates) != 1 || updates[0].Message.From.ID != 111 {
		t.Fatalf("invalid updates %+v %v", updates, err)
	}
	if err := client.Send(context.Background(), 111, "<b>обычный текст</b>"); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramErrorsDoNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(writer, "sensitive-token")
	}))
	defer server.Close()
	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	err := client.Send(context.Background(), 111, "test")
	if err == nil || strings.Contains(err.Error(), "sensitive-token") {
		t.Fatalf("unexpected error %v", err)
	}
}
