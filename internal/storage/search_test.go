package storage

import (
	"context"
	"strings"
	"testing"

	"familyexpenses/internal/bot"
	"familyexpenses/internal/expenses"
	"familyexpenses/internal/llm"
)

func TestSearchAlternativesRespectScopePeriodAndCountEachExpenseOnce(t *testing.T) {
	ctx := context.Background()
	repo := Repository{DB: testDatabase(t)}
	service := expenses.Service{Repo: repo}
	for _, item := range []struct {
		owner                           int64
		amount, date, category, comment string
	}{
		{111, "3.60", "2026-10-19", "еда", "Бургер"},
		{111, "5", "2026-10-19", "еда", "Картошка фри"},
		{111, "7", "2026-10-19", "еда", "Бургер и картофель фри"},
		{111, "8", "2026-10-19", "еда", "Чипсы"},
		{111, "2", "2026-10-19", "еда", "Кофе"},
		{111, "100", "2026-10-18", "еда", "Бургер"},
		{222, "200", "2026-10-19", "еда", "Бургер"},
		{111, "300", "2026-10-19", "покупки", "Игрушка бургер"},
	} {
		_, err := service.Create(ctx, item.owner, expenses.Input{Amount: item.amount, SpentAt: item.date, Category: item.category, Comment: item.comment})
		if err != nil {
			t.Fatal(err)
		}
	}
	category := "еда"
	action := llm.Action{Name: "summarize_expenses", Subject: "self", Category: &category, From: "2026-10-19", To: "2026-10-19", SearchAny: []string{"бургер", "картошка фри", "картофель фри", "бургер"}}
	reply, _, err := (bot.Engine{Currency: "RUB"}).Apply(ctx, service, 111, bot.Prepared{Action: action})
	if err != nil || !strings.Contains(reply, "Всего: 15.60 RUB · расходов: 3") || !strings.Contains(reply, "Поиск по описаниям:") {
		t.Fatalf("incorrect semantic summary: %q %v", reply, err)
	}
	filter := expenses.Filter{UserID: 222, From: action.From, To: action.To, Category: category, SearchAny: action.SearchAny, Limit: 1}
	page, err := service.List(ctx, 111, filter)
	if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].UserID != 111 {
		t.Fatalf("list disagrees with summary or escaped scope: %+v %v", page, err)
	}
	filter.Offset = 1
	next, err := service.List(ctx, 111, filter)
	if err != nil || next.Total != 3 || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatalf("wrong second page: %+v %v", next, err)
	}
	// Alternatives are literal substrings, not SQL, LIKE wildcards, or a regex.
	filter.SearchAny = []string{"%", "_", "' OR TRUE --", "бургер|кофе"}
	zero, err := service.Summary(ctx, 111, filter)
	if err != nil || zero.Count != 0 {
		t.Fatalf("search interpreted syntax: %+v %v", zero, err)
	}
	action.SearchAny = []string{"несуществующий товар"}
	reply, _, err = (bot.Engine{}).Apply(ctx, service, 111, bot.Prepared{Action: action})
	if err != nil || !strings.Contains(reply, "По выбранным условиям расходы не найдены") || !strings.Contains(reply, "несуществующий товар") {
		t.Fatalf("empty search must explain its scope: %q %v", reply, err)
	}
}
