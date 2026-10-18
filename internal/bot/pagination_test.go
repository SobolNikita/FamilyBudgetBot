package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

type pagesRepo struct {
	expenses.Repository
	filters []expenses.Filter
	total   int
}

func (repo *pagesRepo) List(_ context.Context, filter expenses.Filter) (expenses.Page, error) {
	repo.filters = append(repo.filters, filter)
	items := []expenses.Expense{}
	for i := filter.Offset; i < min(filter.Offset+filter.Limit, repo.total); i++ {
		items = append(items, expenses.Expense{ID: int64(i + 1), UserID: filter.UserID, Amount: "1.00", Category: "еда", SpentAt: "2026-06-15", Version: 1})
	}
	return expenses.Page{Items: items, Total: repo.total}, nil
}

func TestContinuationPreservesFiltersAcrossHistoryTrimmingAndRestart(t *testing.T) {
	for _, period := range []struct{ from, to string }{{"2026-06-01", "2026-06-30"}, {"2026-01-01", "2026-12-31"}} {
		t.Run(period.from, func(t *testing.T) {
			repo := &pagesRepo{total: 125}
			service := expenses.Service{Repo: repo}
			var state State
			seen := map[int64]bool{}
			category := "еда"
			for page := 0; page < 7; page++ {
				text := "покажи ещё"
				action := llm.Action{Name: "continue_expenses", Subject: "self", From: "2099-01-01", Offset: 999}
				if page == 0 {
					text = fmt.Sprintf("покажи кофе с %s по %s", period.from, period.to)
					action = llm.Action{Name: "list_expenses", Subject: "self", From: period.from, To: period.to, Category: &category, Search: "кофе"}
				}
				_, next, err := (Engine{}).Apply(context.Background(), service, 111, Prepared{Text: text, State: state, Action: action})
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range next.Context.Shown {
					if seen[item.ID] {
						t.Fatalf("page repeated expense %d", item.ID)
					}
					seen[item.ID] = true
				}
				// Persist/reload between every page, as after a worker restart.
				data, err := json.Marshal(next)
				if err != nil {
					t.Fatal(err)
				}
				state = State{}
				if err := json.Unmarshal(data, &state); err != nil {
					t.Fatal(err)
				}
			}
			if len(seen) != 125 || len(state.Context.Turns) != 4 || state.Context.List.HasMore {
				t.Fatalf("bad final state: %+v", state)
			}
			for page, filter := range repo.filters {
				if filter.UserID != 111 || filter.From != period.from || filter.To != period.to || filter.Category != category || filter.Search != "кофе" || filter.Offset != page*20 || filter.Limit != 20 {
					t.Fatalf("page %d lost filter: %+v", page, filter)
				}
			}
			reply, _, err := (Engine{}).Apply(context.Background(), service, 111, Prepared{State: state, Action: llm.Action{Name: "continue_expenses", Subject: "self"}})
			if err != nil || !strings.Contains(reply, "Больше расходов") || len(repo.filters) != 7 {
				t.Fatalf("end of list repeated query: %q %v", reply, err)
			}
			_, state, err = (Engine{}).Apply(context.Background(), service, 111, Prepared{State: state, Action: llm.Action{Name: "list_expenses", Subject: "self", From: "2026-05-01"}})
			if err != nil || state.Context.List.Filter.From != "2026-05-01" || state.Context.List.NextOffset != 20 {
				t.Fatalf("new query failed to reset cursor: %+v %v", state, err)
			}
		})
	}
}

func TestContinuationWithoutListDoesNotGuessFilters(t *testing.T) {
	// Also covers conversations persisted before ListState was introduced.
	reply, _, err := (Engine{}).Apply(context.Background(), expenses.Service{}, 111, Prepared{Action: llm.Action{Name: "continue_expenses", Subject: "self"}})
	if err != nil || !strings.Contains(reply, "Сначала укажите") {
		t.Fatalf("missing clarification: %q %v", reply, err)
	}
}

func TestContinuationHandlesRowsRemovedBetweenPages(t *testing.T) {
	repo := &pagesRepo{total: 40}
	engine := Engine{}
	service := expenses.Service{Repo: repo}
	_, state, err := engine.Apply(context.Background(), service, 111, Prepared{Action: llm.Action{Name: "list_expenses", Subject: "self"}})
	if err != nil {
		t.Fatal(err)
	}
	repo.total = 10
	reply, state, err := engine.Apply(context.Background(), service, 111, Prepared{State: state, Action: llm.Action{Name: "continue_expenses", Subject: "self"}})
	if err != nil || state.Context.List.HasMore || !strings.Contains(reply, "Больше расходов") {
		t.Fatalf("empty continuation is not terminated: %q %v", reply, err)
	}
}
