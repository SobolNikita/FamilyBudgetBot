package llm

import (
	"context"
	"sync"
	"time"
)

// Circuit suppresses repeated calls during provider-wide outages. In-flight calls
// remain independently bounded by the client timeout.
type Circuit struct {
	Planner Planner
	mu      sync.Mutex
	until   time.Time
	failure *Failure
}

func (c *Circuit) Plan(ctx context.Context, input Request) (Action, error) {
	c.mu.Lock()
	if time.Now().Before(c.until) {
		failure := *c.failure
		c.mu.Unlock()
		return Action{}, &failure
	}
	c.mu.Unlock()
	action, err := c.Planner.Plan(ctx, input)
	if err != nil && ctx.Err() == nil {
		failure := Diagnose(err)
		var delay time.Duration
		switch failure.Kind {
		case "authentication", "permission", "model_or_endpoint":
			delay = 5 * time.Minute
		case "rate_limit":
			delay = time.Minute
		case "unavailable", "connection", "dns", "tls":
			delay = 5 * time.Second
		}
		if delay > 0 {
			c.mu.Lock()
			deadline := time.Now().Add(delay)
			if deadline.After(c.until) {
				c.until = deadline
				copy := *failure
				c.failure = &copy
			}
			c.mu.Unlock()
		}
	}
	return action, err
}
