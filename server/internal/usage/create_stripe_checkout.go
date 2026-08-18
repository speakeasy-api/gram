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

const (
	minimumStripeCheckoutTrialLead       = 48 * time.Hour
	minimumStripeCheckoutSessionLifetime = 30 * time.Minute
	maximumStripeCheckoutSessionLifetime = 24 * time.Hour
	stripeCheckoutExpirySafetyMargin     = time.Minute
)

type stripeCheckoutIntent struct {
	idempotencyKey     string
	billingCycleAnchor time.Time
	trialEnd           *time.Time
	expiresAt          time.Time
}

type preparedStripeCheckoutIntent struct {
	stripeCheckoutIntent
	customerID string
}

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

	now := time.Now().UTC().Truncate(time.Second)
	var productTrialEnd *time.Time
	trial, err := trialsrepo.New(s.db).GetActiveTrial(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		end := trial.EndsAt.Time.UTC()
		productTrialEnd = &end
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "failed to check the active trial").LogError(ctx, s.logger)
	}
	proposedIntent := newStripeCheckoutIntent(authCtx.ActiveOrganizationID, now, productTrialEnd)
	if proposedIntent.trialEnd != nil && proposedIntent.trialEnd.Sub(now) < minimumStripeCheckoutTrialLead {
		return "", oops.E(oops.CodeConflict, nil, "the active trial ends too soon to start self-serve billing").LogWarn(ctx, s.logger)
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
	replaceExpiredSessionID, err := s.expiredCheckoutSessionForReplacement(ctx, billingMetadata, customerID, now)
	if err != nil {
		return "", err
	}

	preparedIntent, err := s.prepareStripeCheckoutIntent(ctx, authCtx.ActiveOrganizationID, customerID, now, proposedIntent, replaceExpiredSessionID)
	if err != nil {
		return "", err
	}

	billingURL := s.siteURL.JoinPath(authCtx.OrganizationSlug, "billing").String()
	checkout, err := s.stripeClient.CreateCheckoutSession(ctx, stripeclient.CreateCheckoutSessionInput{
		CustomerID:         preparedIntent.customerID,
		OrganizationID:     authCtx.ActiveOrganizationID,
		OrganizationSlug:   authCtx.OrganizationSlug,
		SuccessURL:         billingURL,
		CancelURL:          billingURL,
		TrialEnd:           preparedIntent.trialEnd,
		BillingCycleAnchor: preparedIntent.billingCycleAnchor,
		ExpiresAt:          preparedIntent.expiresAt,
		IdempotencyKey:     preparedIntent.idempotencyKey,
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create Stripe Checkout session").LogError(ctx, s.logger)
	}
	if checkout.ID == "" || checkout.URL == "" {
		return "", oops.E(oops.CodeUnexpected, nil, "Stripe Checkout did not return a complete hosted session").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to finalize Stripe Checkout").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	finalized, err := repo.New(dbtx).FinalizeStripeCheckoutIntent(ctx, repo.FinalizeStripeCheckoutIntentParams{
		StripeCheckoutSessionID:          checkout.ID,
		OrganizationID:                   authCtx.ActiveOrganizationID,
		StripeCustomerID:                 preparedIntent.customerID,
		StripeCheckoutIdempotencyKey:     preparedIntent.idempotencyKey,
		StripeCheckoutBillingCycleAnchor: finiteTimestamptz(preparedIntent.billingCycleAnchor),
		StripeCheckoutTrialEnd:           optionalTimestamptz(preparedIntent.trialEnd),
		StripeCheckoutExpiresAt:          finiteTimestamptz(preparedIntent.expiresAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", oops.E(oops.CodeConflict, nil, "billing state changed while Checkout was being created").LogWarn(ctx, s.logger)
		}
		return "", oops.E(oops.CodeUnexpected, err, "failed to finalize Stripe Checkout").LogError(ctx, s.logger)
	}

	if finalized.AttachedNewSession {
		if err := s.auditLogger.LogBillingMetadataCreateStripeCheckout(ctx, dbtx, audit.LogBillingMetadataCreateStripeCheckoutEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			BillingMetadataURN: urn.NewBillingMetadata(finalized.BillingMetadataID),
		}); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to record Stripe Checkout audit event").LogError(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to finalize Stripe Checkout").LogError(ctx, s.logger)
	}

	return checkout.URL, nil
}

func (s *Service) prepareStripeCheckoutIntent(
	ctx context.Context,
	organizationID string,
	customerID string,
	preparedAt time.Time,
	proposed stripeCheckoutIntent,
	replaceExpiredSessionID pgtype.Text,
) (preparedStripeCheckoutIntent, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return preparedStripeCheckoutIntent{}, oops.E(oops.CodeUnexpected, err, "failed to prepare Stripe Checkout").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	stored, err := queries.StoreStripeCustomer(ctx, repo.StoreStripeCustomerParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: customerID, Valid: true},
	})
	if err != nil {
		return preparedStripeCheckoutIntent{}, oops.E(oops.CodeUnexpected, err, "failed to store Stripe customer").LogError(ctx, s.logger)
	}
	if !stored.StripeCustomerID.Valid || stored.StripeCustomerID.String != customerID {
		return preparedStripeCheckoutIntent{}, oops.E(oops.CodeConflict, nil, "billing customer changed while Checkout was being prepared").LogWarn(ctx, s.logger)
	}

	prepared, err := queries.PrepareStripeCheckoutIntent(ctx, repo.PrepareStripeCheckoutIntentParams{
		StripeCheckoutIdempotencyKey:     proposed.idempotencyKey,
		StripeCheckoutBillingCycleAnchor: finiteTimestamptz(proposed.billingCycleAnchor),
		StripeCheckoutTrialEnd:           optionalTimestamptz(proposed.trialEnd),
		StripeCheckoutExpiresAt:          finiteTimestamptz(proposed.expiresAt),
		PreparedAt:                       finiteTimestamptz(preparedAt),
		OrganizationID:                   organizationID,
		StripeCustomerID:                 customerID,
		ReplaceExpiredSessionID:          replaceExpiredSessionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return preparedStripeCheckoutIntent{}, oops.E(oops.CodeConflict, nil, "billing state changed while Checkout was being prepared").LogWarn(ctx, s.logger)
		}
		return preparedStripeCheckoutIntent{}, oops.E(oops.CodeUnexpected, err, "failed to prepare Stripe Checkout").LogError(ctx, s.logger)
	}

	intent, err := checkoutIntentFromRow(prepared)
	if err != nil {
		return preparedStripeCheckoutIntent{}, oops.E(oops.CodeUnexpected, err, "stored Stripe Checkout intent is incomplete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return preparedStripeCheckoutIntent{}, oops.E(oops.CodeUnexpected, err, "failed to prepare Stripe Checkout").LogError(ctx, s.logger)
	}

	return preparedStripeCheckoutIntent{
		stripeCheckoutIntent: intent,
		customerID:           customerID,
	}, nil
}

func (s *Service) expiredCheckoutSessionForReplacement(
	ctx context.Context,
	metadata repo.BillingMetadatum,
	customerID string,
	now time.Time,
) (pgtype.Text, error) {
	if !metadata.StripeCheckoutSessionID.Valid ||
		!metadata.StripeCheckoutExpiresAt.Valid ||
		metadata.StripeCheckoutExpiresAt.Time.After(now) {
		return pgtype.Text{String: "", Valid: false}, nil
	}

	sessionID := metadata.StripeCheckoutSessionID.String
	state, err := s.stripeClient.GetCheckoutSession(ctx, sessionID)
	if err != nil {
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeUnavailable, err, "failed to verify the previous Stripe Checkout session").LogWarn(ctx, s.logger)
	}
	if state == nil || state.ID != sessionID || state.CustomerID != customerID {
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeUnavailable, nil, "previous Stripe Checkout state does not match billing metadata").LogWarn(ctx, s.logger)
	}
	if state.Status != "expired" || state.SubscriptionID != "" {
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeConflict, nil, "the previous Stripe Checkout session is still being reconciled").LogWarn(ctx, s.logger)
	}

	return pgtype.Text{String: sessionID, Valid: true}, nil
}

func checkoutIntentFromRow(row repo.PrepareStripeCheckoutIntentRow) (stripeCheckoutIntent, error) {
	if !row.StripeCheckoutIdempotencyKey.Valid ||
		!row.StripeCheckoutBillingCycleAnchor.Valid ||
		!row.StripeCheckoutExpiresAt.Valid {
		return stripeCheckoutIntent{}, errors.New("required Checkout intent field is null")
	}

	var trialEnd *time.Time
	if row.StripeCheckoutTrialEnd.Valid {
		value := row.StripeCheckoutTrialEnd.Time.UTC()
		trialEnd = &value
	}

	return stripeCheckoutIntent{
		idempotencyKey:     row.StripeCheckoutIdempotencyKey.String,
		billingCycleAnchor: row.StripeCheckoutBillingCycleAnchor.Time.UTC(),
		trialEnd:           trialEnd,
		expiresAt:          row.StripeCheckoutExpiresAt.Time.UTC(),
	}, nil
}

func newStripeCheckoutIntent(organizationID string, now time.Time, productTrialEnd *time.Time) stripeCheckoutIntent {
	now = now.UTC().Truncate(time.Second)
	anchor := nextStripeBillingCycleAnchor(now, productTrialEnd)
	if productTrialEnd == nil && anchor.Sub(now) < minimumStripeCheckoutSessionLifetime+stripeCheckoutExpirySafetyMargin {
		anchor = anchor.AddDate(0, 0, 1)
	}

	expiresAt := anchor.Add(-stripeCheckoutExpirySafetyMargin)
	latestExpiration := now.Add(maximumStripeCheckoutSessionLifetime - stripeCheckoutExpirySafetyMargin)
	if latestExpiration.Before(expiresAt) {
		expiresAt = latestExpiration
	}

	var stripeTrialEnd *time.Time
	if productTrialEnd != nil {
		alignedTrialEnd := anchor
		stripeTrialEnd = &alignedTrialEnd
	}

	return stripeCheckoutIntent{
		idempotencyKey:     fmt.Sprintf("checkout-session:%s:%d:%d", organizationID, anchor.Unix(), expiresAt.Unix()),
		billingCycleAnchor: anchor,
		trialEnd:           stripeTrialEnd,
		expiresAt:          expiresAt,
	}
}

func finiteTimestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), InfinityModifier: pgtype.Finite, Valid: true}
}

func optionalTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}
	return finiteTimestamptz(*value)
}

func nextStripeBillingCycleAnchor(now time.Time, trialEnd *time.Time) time.Time {
	start := now.UTC()
	if trialEnd != nil {
		start = trialEnd.UTC()
	}

	midnight := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	if trialEnd != nil && start.Equal(midnight) {
		return midnight
	}
	return midnight.AddDate(0, 0, 1)
}
