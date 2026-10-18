package httpapi

import (
	"errors"
	"familyexpenses/internal/expenses"
	"fmt"
	"net/http"
	"strconv"
)

func (server Server) changeMember(w http.ResponseWriter, r *http.Request, restore bool) {
	id, err := parseID(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil || version < 1 {
		respond(w, nil, fmt.Errorf("%w: обновите список участников", expenses.ErrInvalid))
		return
	}
	if restore {
		err = server.Members.RestoreUser(r.Context(), id, version)
	} else {
		err = server.Members.RemoveUser(r.Context(), id, version)
	}
	if err != nil {
		if errors.Is(err, expenses.ErrConflict) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Состояние участника изменилось. Обновите список участников перед повтором."})
			return
		}
		respond(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
