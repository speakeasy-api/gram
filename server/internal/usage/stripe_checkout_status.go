package usage

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

var stripeCheckoutSessionIDPattern = regexp.MustCompile(`^cs_[A-Za-z0-9_]+$`)

// GetStripeCheckoutStatus reads Stripe state without activating billing. A return
// URL is only a session identifier, never evidence of successful payment.
func (s *Service) GetStripeCheckoutStatus(ctx context.Context, payload *gen.GetStripeCheckoutStatusPayload) (*gen.GetStripeCheckoutStatusResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	for _, scope := range []authz.Scope{authz.ScopeOrgAdmin, authz.ScopeOrgRead} {
		if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
			return nil, err
		}
	}
	if payload == nil || len(payload.SessionID) > 255 || !stripeCheckoutSessionIDPattern.MatchString(payload.SessionID) {
		return nil, oops.C(oops.CodeBadRequest)
	}
	metadata, err := repo.New(s.db).GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read billing metadata")
	}
	if !metadata.StripeCustomerID.Valid || metadata.StripeCustomerID.String == "" {
		return nil, oops.C(oops.CodeNotFound)
	}
	if s.stripeClient == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	state, err := s.stripeClient.GetCheckoutSession(ctx, payload.SessionID)
	// Missing and foreign sessions deliberately have the same public response.
	if err != nil || state == nil || state.ID != payload.SessionID || state.CustomerID != metadata.StripeCustomerID.String || state.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeNotFound)
	}
	switch state.Status {
	case "open", "complete", "expired":
	default:
		return nil, oops.C(oops.CodeUnavailable)
	}
	switch state.PaymentStatus {
	case "paid", "unpaid", "no_payment_required":
	default:
		return nil, oops.C(oops.CodeUnavailable)
	}
	// Readiness is derived only from this session's expanded subscription, not
	// another locally active subscription or a return URL supplied by the caller.
	billingReady := state.Status == "complete" && state.PaymentReady() &&
		state.SubscriptionID != "" && state.SubscriptionCustomerID == state.CustomerID &&
		!state.BillingCycleAnchor.IsZero() &&
		(state.SubscriptionStatus == "active" || state.SubscriptionStatus == "trialing" || state.SubscriptionStatus == "past_due")
	return &gen.GetStripeCheckoutStatusResult{Status: state.Status, PaymentStatus: state.PaymentStatus, BillingReady: billingReady}, nil
}
