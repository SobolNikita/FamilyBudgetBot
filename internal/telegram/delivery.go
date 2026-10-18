package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"familyexpenses/internal/storage"
	"github.com/jackc/pgx/v5"
)

const receiptText = "Секунду, разбираюсь. Ответ появится здесь — повторять сообщение не нужно."

func (runner Runner) deliver(ctx context.Context) {
	repo := runner.repository()
	for ctx.Err() == nil {
		update, err := repo.NextDelivery(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			if !pause(ctx, 200*time.Millisecond) {
				return
			}
			continue
		}
		if err != nil {
			slog.Error("delivery queue read failed")
			if !pause(ctx, time.Second) {
				return
			}
			continue
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err = repo.TouchDelivery(deliveryCtx, update)
		if err == nil {
			err = runner.deliverUpdate(deliveryCtx, repo, update)
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			code := "telegram_delivery_failed"
			var retryAfter time.Duration
			var failure *APIError
			if errors.As(err, &failure) {
				code = fmt.Sprintf("telegram_http_%d", failure.Status)
				retryAfter = failure.RetryAfter
			}
			if failure != nil && (failure.Status == 400 || failure.Status == 403) {
				if err := repo.SuspendDelivery(ctx, update, code, failure.Status == 403); err != nil {
					slog.Error("delivery suspension failed")
				}
				continue
			}
			if failure != nil && failure.Status == 401 {
				retryAfter = max(retryAfter, time.Minute)
			}
			slog.Warn("Telegram delivery failed", "update_id", update.ID, "code", code, "attempt", update.SendAttempts+1)
			if retryErr := repo.RetryDelivery(ctx, update, retryAfter, code); retryErr != nil {
				slog.Error("delivery retry scheduling failed")
			}
			if retryAfter > 0 && !pause(ctx, retryAfter) {
				return
			}
		}
		if !pause(ctx, time.Second) {
			return
		}
	}
}

func (runner Runner) deliverUpdate(ctx context.Context, repo storage.Repository, update storage.Update) (result error) {
	defer func() { runner.Health.delivery(result) }()
	return repo.DeliverCurrent(ctx, update.ID, func(current storage.Repository, fresh storage.Update) error {
		return runner.deliverCurrent(ctx, current, fresh)
	})
}

func (runner Runner) deliverCurrent(ctx context.Context, repo storage.Repository, update storage.Update) error {
	text := update.Reply
	if update.Status == "pending" {
		text = receiptText
	}
	if update.ReplyMessageID == 0 {
		messageID, err := runner.Client.Reply(ctx, update.ChatID, update.MessageID, text)
		if err != nil {
			return err
		}
		if err := repo.Acknowledge(ctx, update.ID, messageID); err != nil {
			return err
		}
	} else {
		if err := runner.Client.Edit(ctx, update.ChatID, update.ReplyMessageID, text); err != nil {
			var failure *APIError
			if errors.As(err, &failure) && failure.MissingMessage {
				return repo.Acknowledge(ctx, update.ID, 0)
			}
			return err
		}
	}
	if update.Status == "processed" {
		return repo.MarkSent(ctx, update.ID)
	}
	return nil
}
