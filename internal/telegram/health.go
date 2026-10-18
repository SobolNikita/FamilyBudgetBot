package telegram

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type Health struct {
	mu                                          sync.Mutex
	pollAt                                      time.Time
	pollCode, workerCode, llmCode, deliveryCode string
}
type HealthSnapshot struct {
	Ready        bool      `json:"ready"`
	LastPoll     time.Time `json:"last_poll"`
	PollCode     string    `json:"poll_code"`
	WorkerCode   string    `json:"worker_code"`
	LLMCode      string    `json:"llm_code"`
	DeliveryCode string    `json:"delivery_code"`
}

func (h *Health) poll(code string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pollCode = code
	if code == "" {
		h.pollAt = time.Now()
	}
}
func (h *Health) Worker(code string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workerCode = code
}
func (h *Health) llm(code string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.llmCode = code
}

// Delivery failures must not overwrite the lifecycle status of the worker.
// A failure specific to one chat/message is tracked in the persistent queue.
func (h *Health) delivery(err error) {
	if h == nil {
		return
	}
	code := ""
	if err != nil {
		code = "delivery_failed"
		var failure *APIError
		if errors.As(err, &failure) {
			if failure.Status == 400 || failure.Status == 403 {
				code = ""
			} else {
				code = fmt.Sprintf("telegram_http_%d", failure.Status)
			}
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deliveryCode = code
}

func (h *Health) Snapshot() HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HealthSnapshot{Ready: h.workerCode == "" && h.pollCode == "" && !h.pollAt.IsZero() && time.Since(h.pollAt) < time.Minute && h.llmCode == "" && h.deliveryCode == "", LastPoll: h.pollAt, PollCode: h.pollCode, WorkerCode: h.workerCode, LLMCode: h.llmCode, DeliveryCode: h.deliveryCode}
}
