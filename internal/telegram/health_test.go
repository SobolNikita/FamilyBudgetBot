package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"familyexpenses/internal/dialog"
	"familyexpenses/internal/storage"
)

func TestDeliveryRecoveryDoesNotClearOtherHealthFailures(t *testing.T) {
	h := &Health{}
	h.poll("")
	h.delivery(&APIError{Status: 401})
	if h.Snapshot().Ready || h.Snapshot().DeliveryCode != "telegram_http_401" || h.Snapshot().WorkerCode != "" {
		t.Fatal("delivery must have its own failing status")
	}
	h.poll("")
	h.llm("")
	if h.Snapshot().Ready {
		t.Fatal("unrelated success cleared delivery failure")
	}
	h.Worker("stopped")
	h.delivery(nil)
	if h.Snapshot().Ready || h.Snapshot().WorkerCode != "stopped" || h.Snapshot().DeliveryCode != "" {
		t.Fatal("delivery success cleared worker failure")
	}
	h.Worker("")
	h.llm("timeout")
	h.delivery(nil)
	if h.Snapshot().Ready {
		t.Fatal("delivery success cleared LLM failure")
	}
	h.llm("")
	if !h.Snapshot().Ready {
		t.Fatal("health did not recover")
	}
	for _, status := range []int{400, 403} {
		h.delivery(&APIError{Status: status})
		if !h.Snapshot().Ready {
			t.Fatalf("one chat failure %d made the entire bot unhealthy", status)
		}
	}
}

func TestSuccessfulDeliveryRestoresHealthAfterUnauthorizedResponse(t *testing.T) {
	pool := telegramTestDatabase(t)
	ctx := context.Background()
	repo := storage.Repository{DB: pool}
	update := storage.Update{ID: 1, UserID: 111, ChatID: 111, Text: "кофе 5", Status: "processed", Reply: "Готово"}
	if err := repo.Enqueue(ctx, update); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, update.ID, update.UserID, update.Reply, dialog.State{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"ok":false,"error_code":401}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
	}))
	defer server.Close()
	health := &Health{}
	health.poll("")
	runner := Runner{Client: &Client{BaseURL: server.URL, HTTP: server.Client()}, Health: health}
	if err := runner.deliverUpdate(ctx, repo, update); err == nil || health.Snapshot().Ready {
		t.Fatal("unauthorized delivery must fail health")
	}
	if err := runner.deliverUpdate(ctx, repo, update); err != nil {
		t.Fatal(err)
	}
	if !health.Snapshot().Ready {
		t.Fatalf("successful delivery did not restore health: %+v", health.Snapshot())
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM telegram_updates WHERE update_id=1`).Scan(&status); err != nil || status != "sent" {
		t.Fatalf("delivery not persisted: %s %v", status, err)
	}
}
