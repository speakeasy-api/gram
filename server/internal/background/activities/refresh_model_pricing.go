package activities

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type RefreshModelPricing struct {
	openRouter *openrouter.OpenRouter
	logger     *slog.Logger
}

func NewRefreshModelPricing(logger *slog.Logger, openRouter *openrouter.OpenRouter) *RefreshModelPricing {
	return &RefreshModelPricing{
		openRouter: openRouter,
		logger:     logger,
	}
}

type RefreshModelPricingArgs struct{}

func (r *RefreshModelPricing) Do(ctx context.Context, args RefreshModelPricingArgs) error {
	if r.openRouter == nil {
		return oops.E(oops.CodeUnexpected, nil, "openrouter client is not configured").Log(ctx, r.logger)
	}

	if err := r.openRouter.FetchAndCacheModelPricing(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to fetch and cache model pricing").Log(ctx, r.logger)
	}

	r.logger.InfoContext(ctx, "successfully refreshed model pricing")
	return nil
}

