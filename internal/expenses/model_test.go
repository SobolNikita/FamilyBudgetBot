package expenses

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeAmount(t *testing.T) {
	for _, example := range []struct{ input, expected string }{{"350", "350.00"}, {"12,5", "12.50"}, {" 000.01 ", "0.01"}, {"999999999999.99", "999999999999.99"}} {
		actual, err := NormalizeAmount(example.input)
		if err != nil || actual != example.expected {
			t.Errorf("NormalizeAmount(%q) = %q, %v", example.input, actual, err)
		}
	}
	for _, input := range []string{"0", "0.00", "-1", "1.001", "1e3", "NaN", "1000000000000", "", ".5"} {
		if _, err := NormalizeAmount(input); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted invalid amount %q", input)
		}
	}
}

type scopeRecorder struct {
	Repository
	input  Input
	filter Filter
	actor  int64
}

func (repo *scopeRecorder) Create(_ context.Context, input Input) (Expense, error) {
	repo.input = input
	return Expense{}, nil
}
func (repo *scopeRecorder) Update(_ context.Context, actor, id int64, input Input) (Expense, error) {
	repo.actor = actor
	repo.input = input
	return Expense{}, nil
}
func (repo *scopeRecorder) Delete(_ context.Context, actor, id int64, version int) error {
	repo.actor = actor
	return nil
}
func (repo *scopeRecorder) List(_ context.Context, filter Filter) (Page, error) {
	repo.filter = filter
	return Page{}, nil
}
func (repo *scopeRecorder) Summary(_ context.Context, filter Filter) (Summary, error) {
	repo.filter = filter
	return Summary{}, nil
}

func TestBotScopeCannotBeOverridden(t *testing.T) {
	repo := &scopeRecorder{}
	service := Service{Repo: repo}
	ctx := context.Background()
	input := Input{UserID: 222, Amount: "10", Category: "еда", SpentAt: "2026-10-16", Version: 1}
	if _, err := service.Create(ctx, 111, input); err != nil {
		t.Fatal(err)
	}
	if repo.input.UserID != 111 {
		t.Fatal("create escaped actor scope")
	}
	if _, err := service.Update(ctx, 111, 1, input); err != nil {
		t.Fatal(err)
	}
	if repo.actor != 111 || repo.input.UserID != 111 {
		t.Fatal("update escaped actor scope")
	}
	if err := service.Delete(ctx, 111, 1, 1); err != nil || repo.actor != 111 {
		t.Fatal("delete escaped actor scope")
	}
	if _, err := service.List(ctx, 111, Filter{UserID: 222}); err != nil || repo.filter.UserID != 111 {
		t.Fatal("list escaped actor scope")
	}
	if _, err := service.Summary(ctx, 111, Filter{UserID: 222}); err != nil || repo.filter.UserID != 111 {
		t.Fatal("summary escaped actor scope")
	}
}

func TestFilterValidation(t *testing.T) {
	for _, filter := range []Filter{{From: "0000-01-01"}, {From: "2026-02-30"}, {From: "2026-10-17", To: "2026-10-16"}, {Category: "секрет"}, {Limit: 101}, {Offset: -1}, {UserID: -1}} {
		if _, err := ValidateFilter(filter); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted invalid filter %+v", filter)
		}
	}
	filter, err := ValidateFilter(Filter{From: "2024-02-29", To: "2024-02-29"})
	if err != nil || filter.Limit != 25 {
		t.Fatalf("unexpected filter: %+v %v", filter, err)
	}
}

func TestSearchAlternativesValidation(t *testing.T) {
	for _, filter := range []Filter{
		{Search: "кофе", SearchAny: []string{"чай"}},
		{SearchAny: make([]string, 21)},
		{SearchAny: []string{""}},
		{SearchAny: []string{" \n "}},
		{SearchAny: []string{"бургер\x00"}},
		{SearchAny: []string{strings.Repeat("я", 201)}},
		{SearchAny: []string{string([]byte{0xff})}},
	} {
		if _, err := ValidateFilter(filter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid alternatives: %+v %v", filter, err)
		}
	}
	if _, err := ValidateFilter(Filter{SearchAny: []string{"бургер", "картошка фри"}}); err != nil {
		t.Fatal(err)
	}
}
