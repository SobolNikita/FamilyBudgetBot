package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReplyCorrelationAndEdit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["chat_id"] != float64(111) || body["parse_mode"] != nil {
			t.Error("invalid chat or markup")
		}
		switch request.URL.Path {
		case "/sendMessage":
			reply := body["reply_parameters"].(map[string]any)
			if reply["message_id"] != float64(7) || reply["allow_sending_without_reply"] != true {
				t.Error("missing correlation")
			}
		case "/editMessageText":
			if body["message_id"] != float64(42) || body["text"] != "Готово" {
				t.Error("wrong edit target")
			}
		default:
			t.Error("unexpected method")
		}
		fmt.Fprint(writer, `{"ok":true,"result":{"message_id":42}}`)
	}))
	defer server.Close()
	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	messageID, err := client.Reply(context.Background(), 111, 7, receiptText)
	if err != nil || messageID != 42 {
		t.Fatalf("reply failed: %d %v", messageID, err)
	}
	if err := client.Edit(context.Background(), 111, messageID, "Готово"); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryAPIRecovery(t *testing.T) {
	for _, sample := range []struct {
		status  int
		body    string
		success bool
		missing bool
		retry   time.Duration
	}{
		{400, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`, true, false, 0},
		{400, `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`, false, true, 0},
		{429, `{"ok":false,"error_code":429,"description":"sensitive","parameters":{"retry_after":90}}`, false, false, 90 * time.Second},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(sample.status)
			fmt.Fprint(writer, sample.body)
		}))
		client := &Client{BaseURL: server.URL, HTTP: server.Client()}
		err := client.Edit(context.Background(), 111, 42, "Готово")
		server.Close()
		if sample.success {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		failure, ok := err.(*APIError)
		if !ok || failure.Status != sample.status || failure.MissingMessage != sample.missing || failure.RetryAfter != sample.retry {
			t.Fatalf("unexpected failure: %v", err)
		}
	}
}
