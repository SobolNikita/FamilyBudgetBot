package llm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
)

type Failure struct {
	Kind   string
	Status int
}

func (failure *Failure) Error() string {
	return fmt.Sprintf("LLM failure: %s (HTTP %d)", failure.Kind, failure.Status)
}

func Diagnose(err error) *Failure {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	if errors.Is(err, context.Canceled) {
		return &Failure{Kind: "canceled"}
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return &Failure{Kind: "timeout"}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &Failure{Kind: "dns"}
	}
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	if errors.As(err, &certificate) || errors.As(err, &authority) {
		return &Failure{Kind: "tls"}
	}
	return &Failure{Kind: "connection"}
}

func httpFailure(status int) *Failure {
	kind := "request"
	switch {
	case status == 401:
		kind = "authentication"
	case status == 403:
		kind = "permission"
	case status == 404:
		kind = "model_or_endpoint"
	case status == 408 || status == 504:
		kind = "timeout"
	case status == 429:
		kind = "rate_limit"
	case status >= 500:
		kind = "unavailable"
	}
	return &Failure{Kind: kind, Status: status}
}

func (failure *Failure) UserMessage() string {
	message := "Сейчас не получается обработать сообщение. Попробуйте немного позже."
	switch failure.Kind {
	case "timeout":
		message = "Помощник не ответил вовремя. Попробуйте ещё раз чуть позже."
	case "authentication":
		message = "Помощник временно недоступен. Сообщите об этом владельцу приложения."
	case "permission":
		message = "Помощник временно недоступен. Сообщите об этом владельцу приложения."
	case "model_or_endpoint":
		message = "Помощник временно недоступен. Сообщите об этом владельцу приложения."
	case "rate_limit":
		message = "Сейчас много запросов. Попробуйте снова через минуту."
	case "unavailable":
		message = "Помощник временно недоступен. Попробуйте немного позже."
	case "request":
		message = "Не получилось обработать сообщение. Попробуйте написать иначе. Если это повторяется, сообщите владельцу приложения."
	case "invalid_response":
		message = "Не получилось разобраться в сообщении. Попробуйте написать иначе, например: «кофе 5 вчера»."
	case "dns", "tls":
		message = "Не удаётся подключиться к помощнику. Попробуйте немного позже."
	}
	return message + " Расходы по этому сообщению не изменены."
}
