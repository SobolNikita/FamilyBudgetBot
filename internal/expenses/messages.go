package expenses

import (
	"errors"
	"strings"
	"unicode"
)

// UserMessage keeps validation advice while hiding internal error categories.
func UserMessage(err error) string {
	switch {
	case errors.Is(err, ErrGone):
		return "Данные этой операции удалены вместе с участником. Обновите список; повтор не восстановит расход."
	case errors.Is(err, ErrNotFound):
		return "Этот расход не найден. Возможно, его уже удалили. Обновите список расходов."
	case errors.Is(err, ErrConflict):
		return "Данные уже изменились. Обновите список расходов и повторите действие."
	case errors.Is(err, ErrInvalid):
		message := strings.ReplaceAll(err.Error(), ErrInvalid.Error()+": ", "")
		if message == ErrInvalid.Error() {
			return "Проверьте данные и попробуйте ещё раз."
		}
		letters := []rune(message)
		if len(letters) > 0 {
			letters[0] = unicode.ToUpper(letters[0])
		}
		return strings.TrimRight(string(letters), ".") + "."
	default:
		return "Не получилось выполнить действие. Попробуйте немного позже."
	}
}
