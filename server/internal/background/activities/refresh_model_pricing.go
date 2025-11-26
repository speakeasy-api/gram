package activities

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type RefreshModelPricing struct {
	openrouter openrouter.Provisioner
	logger     *slog.Logger
}

func NewRefreshModelPricing(logger *slog.Logger, openrouter openrouter.Provisioner) *RefreshModelPricing {
	return &RefreshModelPricing{
		openrouter: openrouter,
		logger:     logger,
	}
}

type RefreshModelPricingArgs struct{}

func (r *RefreshModelPricing) Do(ctx context.Context, args RefreshModelPricingArgs) error {
	if err := r.openrouter.FetchAndCacheModelPricing(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to fetch and cache model pricing").Log(ctx, r.logger)
	}

	r.logger.InfoContext(ctx, "successfully refreshed model pricing")
	return nil
}
