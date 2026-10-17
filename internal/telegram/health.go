package telegram

import (
	"sync"
	"time"
)

type Health struct {
	mu                            sync.Mutex
	pollAt                        time.Time
	pollCode, workerCode, llmCode string
}
type HealthSnapshot struct {
	Ready      bool      `json:"ready"`
	LastPoll   time.Time `json:"last_poll"`
	PollCode   string    `json:"poll_code"`
	WorkerCode string    `json:"worker_code"`
	LLMCode    string    `json:"llm_code"`
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
func (h *Health) Snapshot() HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HealthSnapshot{Ready: h.workerCode == "" && h.pollCode == "" && !h.pollAt.IsZero() && time.Since(h.pollAt) < time.Minute && h.llmCode == "", LastPoll: h.pollAt, PollCode: h.pollCode, WorkerCode: h.workerCode, LLMCode: h.llmCode}
}
