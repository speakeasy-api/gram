package activities

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
)

type RefreshOpenRouterKey struct {
	openRouter openrouter.Provisioner
	logger     *slog.Logger
}

func NewRefreshOpenRouterKey(logger *slog.Logger, db *pgxpool.Pool, openrouter openrouter.Provisioner) *RefreshOpenRouterKey {
	return &RefreshOpenRouterKey{
		openRouter: openrouter,
		logger:     logger,
	}
}

type RefreshOpenRouterKeyArgs struct {
	OrgID string
}

func (o *RefreshOpenRouterKey) Do(ctx context.Context, args RefreshOpenRouterKeyArgs) error {
	limit, err := o.openRouter.RefreshAPIKeyLimit(ctx, args.OrgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error updating openrouter key").Log(ctx, o.logger)
	}

	o.logger.InfoContext(ctx, "refreshed openrouter key limit", slog.Int("limit", limit))

	return nil
}
