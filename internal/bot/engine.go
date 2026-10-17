package bot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

type Turn struct {
	Text  string `json:"text"`
	Reply string `json:"reply"`
}

type Conversation struct {
	LastExpense *expenses.Expense  `json:"last_expense,omitempty"`
	Shown       []expenses.Expense `json:"shown,omitempty"`
	Turns       []Turn             `json:"turns,omitempty"`
}

type Pending struct {
	ID      int64 `json:"id,string"`
	Version int   `json:"version"`
}

type State struct {
	Context Conversation
	Pending *Pending
}

type Prepared struct {
	Action   llm.Action
	State    State
	Snapshot []expenses.Expense
	Text     string
}

type Engine struct{ Currency string }

func Confirmation(text string) string {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(text), ".! ")) {
	case "да", "подтверждаю", "да, удали", "да удали", "удали":
		return "confirm"
	case "нет", "отмена", "отмени", "не надо":
		return "cancel"
	}
	return ""
}

func (engine Engine) Apply(ctx context.Context, service expenses.Service, actor int64, prepared Prepared) (string, State, error) {
	state := prepared.State
	action := prepared.Action
	var reply string
	if state.Pending != nil {
		switch Confirmation(prepared.Text) {
		case "confirm":
			if err := service.Delete(ctx, actor, state.Pending.ID, state.Pending.Version); err != nil {
				return "", state, err
			}
			reply = "Расход удалён."
			if state.Context.LastExpense != nil && state.Context.LastExpense.ID == state.Pending.ID {
				state.Context.LastExpense = nil
			}
			state.Pending = nil
		case "cancel":
			state.Pending = nil
			reply = "Удаление отменено."
		default:
			reply = "Ожидаю подтверждение удаления: ответьте «да» или «нет»."
		}
		return finish(prepared.Text, reply, state)
	}
	filter := expenses.Filter{From: action.From, To: action.To, Search: action.Search, Limit: 20, Offset: action.Offset}
	if action.Category != nil {
		filter.Category = *action.Category
	}
	switch action.Name {
	case "ask_question":
		if strings.TrimSpace(action.Question) == "" || utf8.RuneCountInString(action.Question) > 500 {
			return "", state, fmt.Errorf("%w: не удалось сформулировать уточнение; попробуйте ещё раз", expenses.ErrInvalid)
		}
		reply = action.Question
	case "create_expense":
		if action.Amount == nil || action.Category == nil || action.SpentAt == nil {
			return "", state, fmt.Errorf("%w: нужны сумма, категория и дата", expenses.ErrInvalid)
		}
		input := expenses.Input{Amount: *action.Amount, Category: *action.Category, SpentAt: *action.SpentAt}
		if action.Comment != nil {
			input.Comment = *action.Comment
		}
		expense, err := service.Create(ctx, actor, input)
		if err != nil {
			return "", state, err
		}
		state.Context.LastExpense = &expense
		reply = "Добавлено: " + engine.describe(expense)
	case "update_expense", "delete_expense":
		id, err := strconv.ParseInt(action.ID, 10, 64)
		if err != nil || id <= 0 {
			return "", state, fmt.Errorf("%w: не удалось выбрать расход", expenses.ErrInvalid)
		}
		selected, ok := selectedExpense(prepared, actor, id)
		if !ok {
			return "", state, fmt.Errorf("%w: сначала найдите расход через список", expenses.ErrInvalid)
		}
		current, err := service.Repo.Get(ctx, actor, id)
		if err != nil {
			return "", state, err
		}
		if current.Version != selected.Version {
			return "", state, expenses.ErrConflict
		}
		if action.Name == "delete_expense" {
			state.Pending = &Pending{ID: id, Version: selected.Version}
			reply = "Удалить " + engine.describe(selected) + "? Ответьте «да» или «нет»."
		} else {
			if action.Amount == nil && action.Category == nil && action.SpentAt == nil && action.Comment == nil {
				return "", state, fmt.Errorf("%w: укажите, что изменить", expenses.ErrInvalid)
			}
			input := expenses.Input{Amount: selected.Amount, Category: selected.Category, SpentAt: selected.SpentAt, Comment: selected.Comment, Version: selected.Version}
			if action.Amount != nil {
				input.Amount = *action.Amount
			}
			if action.Category != nil {
				input.Category = *action.Category
			}
			if action.SpentAt != nil {
				input.SpentAt = *action.SpentAt
			}
			if action.Comment != nil {
				input.Comment = *action.Comment
			}
			expense, err := service.Update(ctx, actor, id, input)
			if err != nil {
				return "", state, err
			}
			state.Context.LastExpense = &expense
			reply = "Изменено: " + engine.describe(expense)
		}
	case "list_expenses":
		page, err := service.List(ctx, actor, filter)
		if err != nil {
			return "", state, err
		}
		state.Context.Shown = page.Items
		if page.Total == 0 {
			reply = "За выбранный период расходов нет."
		} else {
			lines := []string{fmt.Sprintf("Найдено расходов: %d. Показано: %d.", page.Total, len(page.Items))}
			for _, expense := range page.Items {
				lines = append(lines, engine.describe(expense))
			}
			if page.Total > action.Offset+len(page.Items) {
				lines = append(lines, "Можно попросить следующую страницу.")
			}
			reply = strings.Join(lines, "\n")
		}
	case "summarize_expenses":
		summary, err := service.Summary(ctx, actor, filter)
		if err != nil {
			return "", state, err
		}
		lines := []string{fmt.Sprintf("Всего: %s %s · расходов: %d", summary.Amount, engine.Currency, summary.Count)}
		sort.Slice(summary.Categories, func(left, right int) bool {
			return summary.Categories[left].Category < summary.Categories[right].Category
		})
		for _, category := range summary.Categories {
			lines = append(lines, fmt.Sprintf("%s: %s %s", category.Category, category.Amount, engine.Currency))
		}
		reply = strings.Join(lines, "\n")
	default:
		return "", state, fmt.Errorf("%w: неизвестное действие", expenses.ErrInvalid)
	}
	return finish(prepared.Text, reply, state)
}

func selectedExpense(prepared Prepared, actor, id int64) (expenses.Expense, bool) {
	candidates := append([]expenses.Expense{}, prepared.Snapshot...)
	candidates = append(candidates, prepared.State.Context.Shown...)
	if prepared.State.Context.LastExpense != nil {
		candidates = append([]expenses.Expense{*prepared.State.Context.LastExpense}, candidates...)
	}
	for _, candidate := range candidates {
		if candidate.ID == id && candidate.UserID == actor {
			return candidate, true
		}
	}
	return expenses.Expense{}, false
}

func finish(text, reply string, state State) (string, State, error) {
	state.Context.Turns = append(state.Context.Turns, Turn{Text: text, Reply: reply})
	if len(state.Context.Turns) > 4 {
		state.Context.Turns = state.Context.Turns[len(state.Context.Turns)-4:]
	}
	return reply, state, nil
}

func (engine Engine) describe(expense expenses.Expense) string {
	comment := []rune(strings.ReplaceAll(expense.Comment, "\n", " "))
	if len(comment) > 48 {
		comment = append(comment[:45], '.', '.', '.')
	}
	result := fmt.Sprintf("#%d · %s · %s %s · %s", expense.ID, expense.SpentAt, expense.Amount, engine.Currency, expense.Category)
	if len(comment) > 0 {
		result += " · " + string(comment)
	}
	return result
}
