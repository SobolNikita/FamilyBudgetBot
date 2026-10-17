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
	message := "Не удалось связаться с сервисом распознавания. Попробуйте позже."
	switch failure.Kind {
	case "timeout":
		message = "Сервис распознавания не ответил вовремя. Попробуйте позже."
	case "authentication":
		message = "Сервис распознавания отклонил ключ доступа. Владельцу бота нужно проверить LLM_API_KEY."
	case "permission":
		message = "Сервис распознавания запретил доступ. Владельцу бота нужно проверить права ключа и доступность модели."
	case "model_or_endpoint":
		message = "Модель или адрес сервиса распознавания не найдены. Владельцу бота нужно проверить LLM_MODEL и LLM_BASE_URL."
	case "rate_limit":
		message = "Достигнут лимит запросов или квоты сервиса распознавания. Попробуйте позже; владельцу бота стоит проверить лимиты."
	case "unavailable":
		message = "Сервис распознавания временно недоступен. Попробуйте позже."
	case "request":
		message = "Сервис распознавания отклонил запрос. Владельцу бота нужно проверить совместимость модели и настройки."
	case "invalid_response":
		message = "Сервис распознавания вернул некорректный ответ. Попробуйте переформулировать сообщение."
	case "dns", "tls":
		message = "Не удалось установить соединение с сервисом распознавания. Владельцу бота нужно проверить сеть и сертификаты."
	}
	return message + " Расходы по этому сообщению не изменены."
}
