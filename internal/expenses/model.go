package expenses

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var Categories = []string{"еда", "транспорт", "жильё", "развлечения", "здоровье", "покупки", "другое"}

var (
	ErrNotFound = errors.New("расход не найден")
	ErrConflict = errors.New("расход уже изменён; обновите данные и повторите действие")
	ErrInvalid  = errors.New("некорректные данные")
	ErrGone     = errors.New("данные операции удалены вместе с участником")
)

type User struct {
	ID          int64      `json:"id,string"`
	DisplayName string     `json:"display_name"`
	Version     int        `json:"version"`
	RemovedAt   *time.Time `json:"removed_at,omitempty"`
}

type Expense struct {
	ID        int64     `json:"id,string"`
	UserID    int64     `json:"user_id,string"`
	Amount    string    `json:"amount"`
	Category  string    `json:"category"`
	SpentAt   string    `json:"spent_at"`
	Comment   string    `json:"comment"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Input struct {
	IdempotencyKey string `json:"-"`
	UserID         int64  `json:"user_id,string"`
	Amount         string `json:"amount"`
	Category       string `json:"category"`
	SpentAt        string `json:"spent_at"`
	Comment        string `json:"comment"`
	Version        int    `json:"version"`
}

type Filter struct {
	UserID   int64  `json:"user_id,string,omitempty"`
	From     string `json:"from"`
	To       string `json:"to"`
	Category string `json:"category"`
	Search   string `json:"search"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

type Page struct {
	Items    []Expense `json:"items"`
	Total    int       `json:"total"`
	Revision int64     `json:"-"`
}

type CategoryTotal struct {
	Category string `json:"category"`
	Amount   string `json:"amount"`
	Count    int    `json:"count"`
}

type Summary struct {
	Amount     string          `json:"amount"`
	Count      int             `json:"count"`
	Categories []CategoryTotal `json:"categories"`
}

type Repository interface {
	Users(context.Context) ([]User, error)
	List(context.Context, Filter) (Page, error)
	Get(context.Context, int64, int64) (Expense, error)
	Create(context.Context, Input) (Expense, error)
	Update(context.Context, int64, int64, Input) (Expense, error)
	Delete(context.Context, int64, int64, int) error
	Summary(context.Context, Filter) (Summary, error)
}

type Service struct{ Repo Repository }

var amountPattern = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,2})?$`)

func NormalizeAmount(raw string) (string, error) {
	value := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	if !amountPattern.MatchString(value) {
		return "", fmt.Errorf("%w: сумма должна быть положительным числом, максимум 12 цифр и 2 знака после запятой", ErrInvalid)
	}
	parts := strings.SplitN(value, ".", 2)
	whole, _ := strconv.ParseInt(parts[0], 10, 64)
	fraction := "00"
	if len(parts) == 2 {
		fraction = (parts[1] + "0")[:2]
	}
	if whole == 0 && fraction == "00" {
		return "", fmt.Errorf("%w: сумма должна быть больше нуля", ErrInvalid)
	}
	return fmt.Sprintf("%d.%s", whole, fraction), nil
}

func ValidCategory(category string) bool {
	for _, allowed := range Categories {
		if allowed == category {
			return true
		}
	}
	return false
}

func ValidateFilter(filter Filter) (Filter, error) {
	if strings.ContainsRune(filter.Search, 0) {
		return filter, fmt.Errorf("%w: поиск содержит недопустимый символ", ErrInvalid)
	}
	if filter.UserID < 0 || filter.Offset < 0 || filter.Offset > 1000000 || filter.Limit < 0 || filter.Limit > 100 {
		return filter, fmt.Errorf("%w: не удалось открыть выбранную страницу; обновите список расходов", ErrInvalid)
	}
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	for _, date := range []string{filter.From, filter.To} {
		if date != "" {
			if !validDate(date) {
				return filter, fmt.Errorf("%w: дата должна иметь формат YYYY-MM-DD", ErrInvalid)
			}
		}
	}
	if filter.From != "" && filter.To != "" && filter.From > filter.To {
		return filter, fmt.Errorf("%w: начало периода позже конца", ErrInvalid)
	}
	if filter.Category != "" && !ValidCategory(filter.Category) {
		return filter, fmt.Errorf("%w: неизвестная категория", ErrInvalid)
	}
	if utf8.RuneCountInString(filter.Search) > 200 {
		return filter, fmt.Errorf("%w: слишком длинный поиск", ErrInvalid)
	}
	return filter, nil
}

func (service Service) validate(input Input, actor int64) (Input, error) {
	if err := ValidateActor(actor); err != nil {
		return input, err
	}
	if actor != FamilyActor {
		input.UserID = actor
	}
	if input.UserID <= 0 {
		return input, fmt.Errorf("%w: выберите участника семьи", ErrInvalid)
	}
	amount, err := NormalizeAmount(input.Amount)
	if err != nil {
		return input, err
	}
	input.Amount = amount
	if !ValidCategory(input.Category) {
		return input, fmt.Errorf("%w: неизвестная категория", ErrInvalid)
	}
	if !validDate(input.SpentAt) {
		return input, fmt.Errorf("%w: проверьте дату расхода", ErrInvalid)
	}
	input.Comment = strings.TrimSpace(input.Comment)
	if strings.ContainsRune(input.Comment, 0) || !utf8.ValidString(input.Comment) {
		return input, fmt.Errorf("%w: комментарий содержит недопустимый символ", ErrInvalid)
	}
	if utf8.RuneCountInString(input.Comment) > 1000 {
		return input, fmt.Errorf("%w: комментарий длиннее 1000 символов", ErrInvalid)
	}
	return input, nil
}

func (service Service) Create(ctx context.Context, actor int64, input Input) (Expense, error) {
	input, err := service.validate(input, actor)
	if err != nil {
		return Expense{}, err
	}
	return service.Repo.Create(ctx, input)
}

func validDate(value string) bool {
	date, err := time.Parse(time.DateOnly, value)
	return err == nil && date.Year() >= 1
}

func (service Service) Update(ctx context.Context, actor, id int64, input Input) (Expense, error) {
	input, err := service.validate(input, actor)
	if err != nil {
		return Expense{}, err
	}
	if input.Version < 1 || id <= 0 {
		return Expense{}, fmt.Errorf("%w: обновите список и выберите расход ещё раз", ErrInvalid)
	}
	return service.Repo.Update(ctx, actor, id, input)
}

func (service Service) Delete(ctx context.Context, actor, id int64, version int) error {
	if err := ValidateActor(actor); err != nil {
		return err
	}
	if id <= 0 || version < 1 {
		return fmt.Errorf("%w: обновите список и выберите расход ещё раз", ErrInvalid)
	}
	return service.Repo.Delete(ctx, actor, id, version)
}

func (service Service) List(ctx context.Context, actor int64, filter Filter) (Page, error) {
	if err := ValidateActor(actor); err != nil {
		return Page{}, err
	}
	if actor != FamilyActor {
		filter.UserID = actor
	}
	filter, err := ValidateFilter(filter)
	if err != nil {
		return Page{}, err
	}
	return service.Repo.List(ctx, filter)
}

func (service Service) Summary(ctx context.Context, actor int64, filter Filter) (Summary, error) {
	if err := ValidateActor(actor); err != nil {
		return Summary{}, err
	}
	if actor != FamilyActor {
		filter.UserID = actor
	}
	filter, err := ValidateFilter(filter)
	if err != nil {
		return Summary{}, err
	}
	return service.Repo.Summary(ctx, filter)
}
