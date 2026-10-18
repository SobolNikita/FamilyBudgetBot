package httpapi

import (
	"context"
	"errors"
	"net/http"

	"familyexpenses/internal/expenses"
)

type SelectionRepository interface {
	SelectExpenses(context.Context, expenses.Filter) (expenses.Selection, error)
	DeleteSelection(context.Context, expenses.Filter, expenses.DeleteSelectionRequest) (int64, error)
}

func (server Server) selectExpenses(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err == nil {
		filter, err = expenses.ValidateFilter(filter)
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	selection, err := server.Selections.SelectExpenses(r.Context(), filter)
	respond(w, selection, err)
}

func (server Server) deleteSelection(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err == nil {
		filter, err = expenses.ValidateFilter(filter)
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	var input expenses.DeleteSelectionRequest
	if err := decodeLimit(w, r, &input, 128*1024); err != nil {
		respond(w, nil, err)
		return
	}
	count, err := server.Selections.DeleteSelection(r.Context(), filter, input)
	if errors.Is(err, expenses.ErrConflict) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Набор расходов изменился. Обновите список и выберите расходы заново."})
		return
	}
	respond(w, map[string]int64{"deleted": count}, err)
}
