package telegram

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

func pollRetry(err error, attempt int) (time.Duration, string) {
	delay := time.Second * time.Duration(1<<min(max(attempt, 1), 6))
	code := "connection"
	var failure *APIError
	if errors.As(err, &failure) {
		code = fmt.Sprintf("telegram_http_%d", failure.Status)
		if failure.Status == 401 || failure.Status == 403 || failure.Status == 409 {
			delay = time.Minute
		}
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
	}
	return delay + time.Duration(rand.IntN(500))*time.Millisecond, code
}
