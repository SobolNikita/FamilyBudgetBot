package bot

import (
	"context"
	"errors"
	"testing"

	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

func TestForeignRequestsNeverExecuteExpenseOperations(t *testing.T) {
	for _, subject := range []string{"other", "family"} {
		for _, action := range []string{"summarize_expenses", "list_expenses", "create_expenses", "update_expense", "delete_expense", "ask_question"} {
			reply, state, err := (Engine{}).Apply(context.Background(), expenses.Service{}, 111, Prepared{
				Text: "Сколько потратила Ева за неделю", Action: llm.Action{Name: action, Subject: subject, Question: "Всего: 999"},
			})
			if err != nil || reply != AccessDeniedReply || len(state.Context.Turns) != 1 || state.Context.Turns[0].Reply != AccessDeniedReply {
				t.Fatalf("foreign request executed or refusal lost: %s %s %q %v", subject, action, reply, err)
			}
		}
	}
}

func TestUnclearOrMissingSubjectNeverDefaultsToOwnReport(t *testing.T) {
	for _, subject := range []string{"", "unknown", "unclear"} {
		reply, _, err := (Engine{}).Apply(context.Background(), expenses.Service{}, 111, Prepared{Action: llm.Action{Name: "summarize_expenses", Subject: subject}})
		if subject == "unclear" {
			if err != nil || reply == "" {
				t.Fatal("missing clarification")
			}
		} else if !errors.Is(err, expenses.ErrInvalid) {
			t.Fatal("missing owner silently accepted")
		}
	}
}
