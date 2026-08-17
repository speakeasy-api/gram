package usage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const maxStripeWebhookBodyBytes = 1 << 20

var acceptedStripeWebhookEvents = map[string]struct{}{
	"checkout.session.completed":    {},
	"customer.subscription.deleted": {},
	"invoice.created":               {},
	"invoice.payment_failed":        {},
}

const paygOpenRouterChatCreditLimit = 100

type stripeWebhookResult struct {
	newlyEnabledFeatures []productfeatures.Feature
}

type stripeWebhookHandler func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error)

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
	if event.Type == "checkout.session.completed" && (event.ObjectID == "" || event.CustomerID == "" || event.SubscriptionID == "") {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe Checkout completion identifiers").LogWarn(ctx, logger)
	}
	if event.CustomerID == "" {
		logger.WarnContext(ctx, "skipping Stripe webhook event without a customer")
		return nil
	}
	received, err := repo.New(s.db).StripeWebhookReceiptExists(ctx, event.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to check Stripe webhook receipt").LogError(ctx, logger)
	}
	if received {
		logger.InfoContext(ctx, "skipping duplicate Stripe webhook event")
		return nil
	}

	var checkoutState *stripeclient.CheckoutSessionState
	checkoutEligible := true
	if event.Type == "checkout.session.completed" {
		checkoutState, checkoutEligible, err = s.currentCheckoutSession(ctx, event)
		if err != nil {
			return err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin Stripe webhook transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	organizationID, err := queries.GetBillingMetadataOrganizationByStripeCustomerID(ctx, pgtype.Text{String: event.CustomerID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping Stripe webhook event for an unknown customer")
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to resolve Stripe webhook customer").LogError(ctx, logger)
	}

	if event.Type == "checkout.session.completed" && checkoutEligible {
		if _, err := trialsrepo.New(tx).MarkTrialConverted(ctx, organizationID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to mark enterprise trial converted").LogError(ctx, logger)
		}
		if err := queries.AcquireStripeSubscriptionActivationLock(ctx, event.SubscriptionID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to lock Stripe subscription activation").LogError(ctx, logger)
		}
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
	if !checkoutEligible {
		if err := tx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to commit Stripe webhook receipt").LogError(ctx, logger)
		}
		return nil
	}

	result, err := s.stripeHandler(ctx, logger, tx, organizationID, event, checkoutState)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to handle Stripe webhook event").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to commit Stripe webhook transaction").LogError(ctx, logger)
	}
	if s.productFeatures != nil {
		for _, enabledFeature := range result.newlyEnabledFeatures {
			s.productFeatures.UpdateFeatureCache(ctx, organizationID, enabledFeature, true)
		}
	}

	return nil
}

func (s *Service) currentCheckoutSession(ctx context.Context, event *stripeclient.WebhookEvent) (*stripeclient.CheckoutSessionState, bool, error) {
	if event.ObjectID == "" || event.SubscriptionID == "" {
		return nil, false, oops.E(oops.CodeBadRequest, nil, "invalid Stripe Checkout completion identifiers").LogWarn(ctx, s.logger)
	}

	state, err := s.stripeClient.GetCheckoutSession(ctx, event.ObjectID)
	if err != nil {
		return nil, false, oops.E(oops.CodeUnavailable, err, "failed to retrieve current Stripe Checkout session").LogWarn(ctx, s.logger)
	}
	if state == nil || state.ID != event.ObjectID || state.CustomerID == "" || state.CustomerID != event.CustomerID || state.SubscriptionID == "" || state.SubscriptionID != event.SubscriptionID || state.SubscriptionCustomerID != event.CustomerID || state.BillingCycleAnchor.IsZero() {
		return nil, false, oops.E(oops.CodeBadRequest, nil, "Stripe Checkout completion identifiers do not match current state").LogWarn(ctx, s.logger)
	}

	switch state.Status {
	case "expired":
		return state, false, nil
	case "complete":
	default:
		return nil, false, oops.E(oops.CodeUnavailable, nil, "Stripe Checkout session is not complete").LogWarn(ctx, s.logger)
	}

	switch state.SubscriptionStatus {
	case "active", "trialing", "past_due":
		return state, true, nil
	case "canceled", "incomplete_expired", "unpaid":
		return state, false, nil
	default:
		return nil, false, oops.E(oops.CodeUnavailable, nil, "Stripe subscription is not ready for activation").LogWarn(ctx, s.logger)
	}
}

func (s *Service) serviceStripeWebhookHandler(ctx context.Context, logger *slog.Logger, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent, checkout *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
	switch event.Type {
	case "checkout.session.completed":
		return s.activatePaygCheckout(ctx, tx, organizationID, event, checkout)
	case "invoice.created":
		logger.InfoContext(ctx, "received Stripe invoice creation")
	case "invoice.payment_failed":
		logger.InfoContext(ctx, "received Stripe invoice payment failure")
	case "customer.subscription.deleted":
		logger.InfoContext(ctx, "received Stripe subscription deletion")
	}
	return stripeWebhookResult{newlyEnabledFeatures: nil}, nil
}

func (s *Service) activatePaygCheckout(ctx context.Context, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent, checkout *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
	if checkout == nil {
		return stripeWebhookResult{}, errors.New("missing current Stripe Checkout state")
	}

	q := repo.New(tx)
	state, err := q.GetPaygActivationState(ctx, organizationID)
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("lock PAYG activation state: %w", err)
	}
	if !state.StripeCustomerID.Valid || state.StripeCustomerID.String != event.CustomerID {
		return stripeWebhookResult{}, errors.New("stripe customer does not own the organization's billing metadata")
	}
	if state.StripeSubscriptionID.Valid && state.StripeSubscriptionID.String != event.SubscriptionID {
		return stripeWebhookResult{}, errors.New("organization already belongs to another Stripe subscription")
	}

	owners, err := q.ListStripeSubscriptionOwners(ctx, pgtype.Text{String: event.SubscriptionID, Valid: true})
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("check Stripe subscription ownership: %w", err)
	}
	switch {
	case len(owners) > 1:
		return stripeWebhookResult{}, errors.New("stripe subscription belongs to multiple organizations")
	case len(owners) == 1 && owners[0] != organizationID:
		return stripeWebhookResult{}, errors.New("stripe subscription already belongs to another organization")
	}

	newlyEnabled := []productfeatures.Feature(nil)
	if _, err := trialsrepo.New(tx).GetTrial(ctx, organizationID); errors.Is(err, pgx.ErrNoRows) {
		newlyEnabled, err = productfeatures.SeedPaygEntitlementsTx(ctx, tx, organizationID)
		if err != nil {
			return stripeWebhookResult{}, fmt.Errorf("seed PAYG entitlements: %w", err)
		}
	} else if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("read trial state for PAYG activation: %w", err)
	}

	anchorDay := conv.SafeInt32(checkout.BillingCycleAnchor.UTC().Day())
	alreadyActivated := state.StripeSubscriptionID.Valid &&
		state.StripeSubscriptionID.String == event.SubscriptionID &&
		state.BillingCycleAnchorDay == anchorDay &&
		state.GramAccountType == "payg" && state.Whitelisted
	if alreadyActivated {
		return stripeWebhookResult{newlyEnabledFeatures: newlyEnabled}, nil
	}

	if _, err := q.ActivatePaygBillingMetadata(ctx, repo.ActivatePaygBillingMetadataParams{
		StripeSubscriptionID:  pgtype.Text{String: event.SubscriptionID, Valid: true},
		BillingCycleAnchorDay: anchorDay,
		OrganizationID:        organizationID,
		StripeCustomerID:      pgtype.Text{String: event.CustomerID, Valid: true},
	}); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("store PAYG Stripe subscription: %w", err)
	}
	if err := q.ActivatePaygOrganization(ctx, organizationID); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("activate PAYG organization: %w", err)
	}

	if s.auditLogger == nil {
		return stripeWebhookResult{}, errors.New("audit logger is unavailable")
	}
	if err := s.auditLogger.LogOrganizationPaygActivated(ctx, tx, audit.LogOrganizationPaygActivatedEvent{
		OrganizationID:   organizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		ActorDisplayName: nil,
		ActorSlug:        nil,
		OrganizationName: state.OrganizationName,
		OrganizationSlug: state.OrganizationSlug,
		OrganizationSnapshotBefore: &audit.OrganizationPaygActivationSnapshot{
			AccountType: state.GramAccountType,
			Whitelisted: state.Whitelisted,
		},
		OrganizationSnapshotAfter: &audit.OrganizationPaygActivationSnapshot{
			AccountType: "payg",
			Whitelisted: true,
		},
	}); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("log PAYG organization activation: %w", err)
	}

	return stripeWebhookResult{newlyEnabledFeatures: newlyEnabled}, nil
}
