package activities

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type CancelAssistantsSubscription struct {
	logger      *slog.Logger
	billingRepo billing.Repository
}

func NewCancelAssistantsSubscription(logger *slog.Logger, billingRepo billing.Repository) *CancelAssistantsSubscription {
	return &CancelAssistantsSubscription{
		logger:      logger,
		billingRepo: billingRepo,
	}
}

type CancelAssistantsSubscriptionArgs struct {
	SubscriptionID string
}

func (a *CancelAssistantsSubscription) Do(ctx context.Context, args CancelAssistantsSubscriptionArgs) error {
	if err := a.billingRepo.CancelSubscriptionAtPeriodEnd(ctx, args.SubscriptionID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "cancel assistants subscription at period end").Log(ctx, a.logger)
	}
	return nil
}
