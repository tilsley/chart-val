package ports

import (
	"context"

	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
)

// DiffUseCase is the driving port for triggering a chart diff.
type DiffUseCase interface {
	Execute(ctx context.Context, pr domain.PRContext) error
}
