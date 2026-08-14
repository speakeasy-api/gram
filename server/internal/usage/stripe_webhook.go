package usage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const maxStripeWebhookBodyBytes = 1 << 20

var acceptedStripeWebhookEvents = map[string]struct{}{
	"checkout.session.completed":    {},
	"customer.subscription.deleted": {},
	"invoice.created":               {},
	"invoice.payment_failed":        {},
}

type stripeWebhookHandler func(context.Context, *slog.Logger, *repo.Queries, string, *stripeclient.WebhookEvent) error

func (s *Service) handleStripeWebhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, r.Body.Close)

	r.Body = http.MaxBytesReader(w, r.Body, maxStripeWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return oops.E(oops.CodeRequestTooLarge, err, "Stripe webhook body is too large").LogWarn(ctx, s.logger)
		}
		return oops.E(oops.CodeBadRequest, err, "invalid Stripe webhook body").LogWarn(ctx, s.logger)
	}

	event, err := s.stripeClient.VerifyWebhook(body, r.Header.Get("Stripe-Signature"))
	if err != nil {
		if errors.Is(err, stripeclient.ErrWebhookNotConfigured) {
			return oops.E(oops.CodeUnavailable, err, "Stripe webhooks are not configured").LogWarn(ctx, s.logger)
		}
		return oops.E(oops.CodeBadRequest, err, "invalid Stripe webhook signature").LogWarn(ctx, s.logger)
	}
	if event == nil || event.ID == "" || event.Type == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe webhook event").LogWarn(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogEvent(event.Type), attr.SlogStripeWebhookEventID(event.ID))
	if _, ok := acceptedStripeWebhookEvents[event.Type]; !ok {
		logger.InfoContext(ctx, "skipping unsupported Stripe webhook event")
		return nil
	}
	if event.CustomerID == "" {
		logger.WarnContext(ctx, "skipping Stripe webhook event without a customer")
		return nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin Stripe webhook transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	organizationID, err := queries.LockBillingMetadataByStripeCustomerID(ctx, pgtype.Text{String: event.CustomerID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping Stripe webhook event for an unknown customer")
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to resolve Stripe webhook customer").LogError(ctx, logger)
	}

	inserted, err := queries.TryInsertStripeWebhookReceipt(ctx, repo.TryInsertStripeWebhookReceiptParams{
		StripeEventID:  event.ID,
		OrganizationID: organizationID,
		EventType:      event.Type,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record Stripe webhook receipt").LogError(ctx, logger)
	}
	if !inserted {
		logger.InfoContext(ctx, "skipping duplicate Stripe webhook event")
		return nil
	}

	if err := s.stripeHandler(ctx, logger, queries, organizationID, event); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to handle Stripe webhook event").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to commit Stripe webhook transaction").LogError(ctx, logger)
	}

	return nil
}

func serviceStripeWebhookHandler(ctx context.Context, logger *slog.Logger, _ *repo.Queries, _ string, event *stripeclient.WebhookEvent) error {
	switch event.Type {
	case "checkout.session.completed":
		logger.InfoContext(ctx, "received Stripe checkout completion")
	case "invoice.created":
		logger.InfoContext(ctx, "received Stripe invoice creation")
	case "invoice.payment_failed":
		logger.InfoContext(ctx, "received Stripe invoice payment failure")
	case "customer.subscription.deleted":
		logger.InfoContext(ctx, "received Stripe subscription deletion")
	}
	return nil
}
