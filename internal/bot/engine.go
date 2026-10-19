package bot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"familyexpenses/internal/dialog"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

type Turn = dialog.Turn
type Conversation = dialog.Conversation
type Pending = dialog.Pending
type State = dialog.State

type Prepared struct {
	Action   llm.Action
	State    State
	Snapshot []expenses.Expense
	Text     string
}

type Engine struct{ Currency string }

const AccessDeniedReply = "Здесь можно посмотреть и изменить только свои расходы. Общие расходы семьи доступны в веб-панели."

func Confirmation(text string) string {
	switch strings.Join(strings.Fields(strings.ToLower(strings.Trim(strings.TrimSpace(text), ".! "))), " ") {
	case "да", "подтверждаю", "да, удали", "да удали", "удали", "да, конечно", "да конечно", "да, удали его", "да удали его", "подтверждаю удаление":
		return "confirm"
	case "нет", "отмена", "отмени", "не надо", "не удаляй", "нет, не удаляй", "нет не удаляй", "оставь", "оставь его", "передумал", "передумала":
		return "cancel"
	}
	return ""
}

func (engine Engine) Apply(ctx context.Context, service expenses.Service, actor int64, prepared Prepared) (string, State, error) {
	if actor <= 0 {
		return "", prepared.State, fmt.Errorf("%w: Telegram требует положительный ID участника", expenses.ErrInvalid)
	}
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
			shown := make([]expenses.Expense, 0, len(state.Context.Shown))
			for _, item := range state.Context.Shown {
				if item.ID != state.Pending.ID {
					shown = append(shown, item)
				}
			}
			state.Context.Shown = shown
			state.Context.Turns = nil
			if state.Context.LastExpense != nil && state.Context.LastExpense.ID == state.Pending.ID {
				state.Context.LastExpense = nil
			}
			state.Pending = nil
		case "cancel":
			state.Pending = nil
			reply = "Удаление отменено."
		default:
			reply = "Сначала завершите удаление: напишите «да» или «не удаляй». Новая покупка из этого сообщения не записана; после отмены пришлите её ещё раз."
		}
		return finish(prepared.Text, reply, state)
	}
	switch action.Subject {
	case "other", "family":
		return finish(prepared.Text, AccessDeniedReply, state)
	case "unclear":
		return finish(prepared.Text, "Вы хотите посмотреть свои расходы? Здесь я могу помочь с вашими записями, а общие расходы семьи доступны в веб-панели.", state)
	case "self":
	default:
		return "", state, fmt.Errorf("%w: не удалось определить владельца расходов; уточните запрос", expenses.ErrInvalid)
	}
	filter := expenses.Filter{From: action.From, To: action.To, Search: action.Search, SearchAny: action.SearchAny, Limit: 20, Offset: action.Offset}
	if action.Category != nil {
		filter.Category = *action.Category
	}
	switch action.Name {
	case "ask_question":
		if strings.TrimSpace(action.Question) == "" || utf8.RuneCountInString(action.Question) > 500 {
			return "", state, fmt.Errorf("%w: не удалось сформулировать уточнение; попробуйте ещё раз", expenses.ErrInvalid)
		}
		reply = action.Question
	case "create_expenses":
		if len(action.Expenses) == 0 || len(action.Expenses) > llm.MaxExpensesPerMessage {
			return "", state, fmt.Errorf("%w: пришлите от 1 до %d расходов в одном сообщении", expenses.ErrInvalid, llm.MaxExpensesPerMessage)
		}
		created := make([]expenses.Expense, 0, len(action.Expenses))
		lines := []string{fmt.Sprintf("Добавлено расходов: %d.", len(action.Expenses))}
		for position, item := range action.Expenses {
			input := expenses.Input{Amount: item.Amount, Category: item.Category, SpentAt: item.SpentAt, Comment: item.Comment}
			expense, err := service.Create(ctx, actor, input)
			if err != nil {
				return "", state, fmt.Errorf("позиция %d: %w", position+1, err)
			}
			created = append(created, expense)
			lines = append(lines, engine.describe(expense))
		}
		state.Context.Shown = created
		state.Context.LastExpense = &created[len(created)-1]
		reply = strings.Join(lines, "\n")
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
	case "list_expenses", "continue_expenses":
		if action.Name == "continue_expenses" {
			if state.Context.List == nil {
				return finish(prepared.Text, "Сначала укажите, какие расходы показать. Например: «покажи расходы за месяц».", state)
			}
			if !state.Context.List.HasMore {
				return finish(prepared.Text, "Больше расходов по этому запросу нет. Чтобы обновить список, укажите период ещё раз.", state)
			}
			filter = state.Context.List.Filter
			filter.Offset = state.Context.List.NextOffset
		}
		page, err := service.List(ctx, actor, filter)
		if err != nil {
			return "", state, err
		}
		if action.Name == "continue_expenses" && (state.Context.List.Revision == 0 || page.Revision != state.Context.List.Revision) {
			state.Context.List = nil
			state.Context.Shown = nil
			return finish(prepared.Text, "Список расходов изменился. Чтобы не пропустить записи, начните просмотр заново: укажите период ещё раз.", state)
		}
		state.Context.Shown = page.Items
		nextOffset := filter.Offset + len(page.Items)
		savedFilter := filter
		savedFilter.UserID, savedFilter.Offset = 0, 0
		state.Context.List = &dialog.ListState{Filter: savedFilter, NextOffset: nextOffset, HasMore: len(page.Items) > 0 && nextOffset < page.Total, Revision: page.Revision}
		for _, item := range page.Items {
			if state.Context.LastExpense != nil && state.Context.LastExpense.ID == item.ID {
				fresh := item
				state.Context.LastExpense = &fresh
			}
		}
		if action.Name == "continue_expenses" && len(page.Items) == 0 {
			reply = "Больше расходов по этому запросу нет. Чтобы обновить список, укажите период ещё раз."
		} else if page.Total == 0 {
			reply = "По выбранным условиям расходы не найдены."
		} else {
			lines := []string{fmt.Sprintf("Найдено расходов: %d. Показано: %d.", page.Total, len(page.Items))}
			for _, expense := range page.Items {
				lines = append(lines, engine.describe(expense))
			}
			if state.Context.List.HasMore {
				lines = append(lines, "Чтобы посмотреть остальные, напишите «покажи ещё».")
			}
			reply = strings.Join(lines, "\n")
		}
		reply += searchDescription(filter)
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
		if summary.Count == 0 {
			reply = "По выбранным условиям расходы не найдены."
		}
		reply += searchDescription(filter)
	default:
		return "", state, fmt.Errorf("%w: неизвестное действие", expenses.ErrInvalid)
	}
	return finish(prepared.Text, reply, state)
}

func searchDescription(filter expenses.Filter) string {
	terms := filter.SearchAny
	if filter.Search != "" {
		terms = []string{filter.Search}
	}
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, len(terms))
	for i, term := range terms {
		quoted[i] = fmt.Sprintf("«%s»", strings.ReplaceAll(term, "\n", " "))
	}
	return "\nПоиск по описаниям: " + strings.Join(quoted, ", ") + "."
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
	return reply, state.WithTurn(Turn{Text: text, Reply: reply}), nil
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
