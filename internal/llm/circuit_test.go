package llm

import (
	"context"
	"testing"
)

type failingPlanner struct {
	calls   int
	failure *Failure
}

func (p *failingPlanner) Plan(context.Context, Request) (Action, error) {
	p.calls++
	return Action{}, p.failure
}
func TestProviderFailureStopsRepeatedCalls(t *testing.T) {
	for _, kind := range []string{"authentication", "permission", "model_or_endpoint", "rate_limit", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			p := &failingPlanner{failure: &Failure{Kind: kind}}
			c := &Circuit{Planner: p}
			for range 10 {
				_, err := c.Plan(context.Background(), Request{})
				if Diagnose(err).Kind != kind {
					t.Fatal("failure changed")
				}
			}
			if p.calls != 1 {
				t.Fatalf("provider called %d times", p.calls)
			}
		})
	}
}
