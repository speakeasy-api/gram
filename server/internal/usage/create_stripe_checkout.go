package usage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const minimumStripeCheckoutTrialLead = 48 * time.Hour

func (s *Service) CreateStripeCheckout(ctx context.Context, _ *gen.CreateStripeCheckoutPayload) (string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", err
	}

	if s.featureFlags == nil {
		return "", oops.E(oops.CodeUnavailable, nil, "self-serve billing is temporarily unavailable").LogWarn(ctx, s.logger)
	}
	enabled, err := s.featureFlags.IsFlagEnabled(
		ctx,
		feature.FlagPaygSelfServeBilling,
		authCtx.ActiveOrganizationID,
		feature.OrgProjectGroups(authCtx.OrganizationSlug, ""),
	)
	if err != nil {
		return "", oops.E(oops.CodeUnavailable, err, "self-serve billing is temporarily unavailable").LogWarn(ctx, s.logger)
	}
	if !enabled {
		return "", oops.E(oops.CodeForbidden, nil, "self-serve billing is not enabled").LogWarn(ctx, s.logger)
	}
	if s.stripeClient == nil || s.siteURL == nil {
		return "", oops.E(oops.CodeUnavailable, nil, "self-serve billing is temporarily unavailable").LogWarn(ctx, s.logger)
	}

	var trialEnd *time.Time
	trial, err := trialsrepo.New(s.db).GetActiveTrial(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		end := trial.EndsAt.Time.UTC()
		if time.Until(end) < minimumStripeCheckoutTrialLead {
			return "", oops.E(oops.CodeConflict, nil, "the active trial ends too soon to start self-serve billing").LogWarn(ctx, s.logger)
		}
		trialEnd = &end
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "failed to check the active trial").LogError(ctx, s.logger)
	}

	billingMetadata, err := repo.New(s.db).GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").LogError(ctx, s.logger)
	}
	if err == nil && billingMetadata.StripeSubscriptionID.Valid {
		return "", oops.E(oops.CodeConflict, nil, "the organization already has a Stripe subscription").LogWarn(ctx, s.logger)
	}

	customerID := ""
	if err == nil && billingMetadata.StripeCustomerID.Valid {
		customerID = billingMetadata.StripeCustomerID.String
	} else {
		customer, err := s.stripeClient.CreateCustomer(ctx, stripeclient.CreateCustomerInput{
			OrganizationID:   authCtx.ActiveOrganizationID,
			OrganizationSlug: authCtx.OrganizationSlug,
			IdempotencyKey:   fmt.Sprintf("customer:%s", authCtx.ActiveOrganizationID),
		})
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to create Stripe customer").LogError(ctx, s.logger)
		}
		customerID = customer.ID
	}

	billingURL := s.siteURL.JoinPath(authCtx.OrganizationSlug, "billing").String()
	checkout, err := s.stripeClient.CreateCheckoutSession(ctx, stripeclient.CreateCheckoutSessionInput{
		CustomerID:       customerID,
		OrganizationID:   authCtx.ActiveOrganizationID,
		OrganizationSlug: authCtx.OrganizationSlug,
		SuccessURL:       billingURL,
		CancelURL:        billingURL,
		TrialEnd:         trialEnd,
		IdempotencyKey:   fmt.Sprintf("checkout-session:%s", authCtx.ActiveOrganizationID),
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create Stripe Checkout session").LogError(ctx, s.logger)
	}
	if checkout.URL == "" {
		return "", oops.E(oops.CodeUnexpected, nil, "Stripe Checkout did not return a hosted URL").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to store Stripe customer").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	stored, err := repo.New(dbtx).StoreStripeCustomer(ctx, repo.StoreStripeCustomerParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		StripeCustomerID: pgtype.Text{String: customerID, Valid: true},
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to store Stripe customer").LogError(ctx, s.logger)
	}
	if !stored.StripeCustomerID.Valid || stored.StripeCustomerID.String != customerID {
		return "", oops.E(oops.CodeConflict, nil, "billing customer changed while Checkout was being created").LogWarn(ctx, s.logger)
	}

	if err := s.auditLogger.LogBillingMetadataCreateStripeCheckout(ctx, dbtx, audit.LogBillingMetadataCreateStripeCheckoutEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		BillingMetadataURN: urn.NewBillingMetadata(stored.ID),
	}); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to record Stripe Checkout audit event").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to store Stripe customer").LogError(ctx, s.logger)
	}

	return checkout.URL, nil
}
