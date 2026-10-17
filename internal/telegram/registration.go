package telegram

import (
	"context"
	"familyexpenses/internal/processing"
	"familyexpenses/internal/storage"
)

func (runner Runner) register(ctx context.Context, update storage.Update) (bool, error) {
	return (processing.Processor{Pool: runner.Pool}).Register(ctx, update)
}
