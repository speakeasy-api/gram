package usage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
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

type stripeCheckoutTrialFingerprint struct {
	organizationID string
	tier           string
	endsAt         time.Time
	convertedAt    *time.Time
	demotedAt      *time.Time
	createdAt      time.Time
	updatedAt      time.Time
}

type preparedStripeCheckoutIntent struct {
	stripeCheckoutIntent
	customerID string
}

// stripeOrganizationIdentity is the organization view stamped onto Stripe customers,
// Checkout sessions and subscriptions so downstream consumers (revenue notifications,
// CRM sync) can attribute them without a Gram DB lookup.
type stripeOrganizationIdentity struct {
	name        string
	accountType string
	email       string
}

// stripeOrganizationIdentity resolves the identity from organization metadata. The
// customer email prefers the billing alert email and falls back to the first
// organization admin; it stays empty when neither exists.
func (s *Service) stripeOrganizationIdentity(ctx context.Context, organizationID string, billingMetadata repo.BillingMetadatum) (stripeOrganizationIdentity, error) {
	organization, err := s.orgRepo.GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return stripeOrganizationIdentity{}, oops.E(oops.CodeUnexpected, err, "failed to load organization for billing").LogError(ctx, s.logger)
	}

	email := ""
	if billingMetadata.AlertEmail.Valid && billingMetadata.AlertEmail.String != "" {
		email = billingMetadata.AlertEmail.String
	} else {
		admins, adminErr := authz.ResolveOrganizationAdminEmails(ctx, s.db, organizationID)
		if adminErr != nil {
			s.logger.WarnContext(ctx, "failed to resolve organization admin emails for Stripe customer", attr.SlogError(adminErr))
		}
		if len(admins) > 0 {
			email = admins[0]
		}
	}

	return stripeOrganizationIdentity{
		name:        organization.Name,
		accountType: organization.GramAccountType,
		email:       email,
	}, nil
}

type stripeCheckoutSessionExpirer interface {
	ExpireCheckoutSession(context.Context, string) error
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

	now := s.checkoutNow()
	var productTrialEnd *time.Time
	var expectedTrial *stripeCheckoutTrialFingerprint
	trial, err := trialsrepo.New(s.db).GetTrial(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		expectedTrial = newStripeCheckoutTrialFingerprint(trial)
		if trial.Tier == "enterprise" && !trial.ConvertedAt.Valid && !trial.DemotedAt.Valid && trial.EndsAt.Valid && trial.EndsAt.Time.After(now) {
			end := trial.EndsAt.Time.UTC()
			productTrialEnd = &end
		}
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "failed to check the trial lifecycle").LogError(ctx, s.logger)
	}
	proposedIntent := newStripeCheckoutIntentForTrial(authCtx.ActiveOrganizationID, now, productTrialEnd, expectedTrial)
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
	// A converted trial with an attached Checkout receipt is an exact replay of
	// the already-committed business transaction, not a stale prepared intent.
	if expectedTrial != nil && expectedTrial.convertedAt != nil && billingMetadata.StripeCheckoutSessionID.Valid {
		storedIntent, storedErr := checkoutIntentFromMetadata(billingMetadata)
		if storedErr != nil {
			return "", oops.E(oops.CodeUnexpected, storedErr, "stored Stripe Checkout receipt is incomplete").LogError(ctx, s.logger)
		}
		proposedIntent = storedIntent
	}

	identity, identityErr := s.stripeOrganizationIdentity(ctx, authCtx.ActiveOrganizationID, billingMetadata)
	if identityErr != nil {
		return "", identityErr
	}

	customerID := ""
	if err == nil && billingMetadata.StripeCustomerID.Valid {
		customerID = billingMetadata.StripeCustomerID.String
		// Best effort: keep identity and contract metadata current on customers created
		// before the contract existed, or whose name/email/account type changed.
		if updateErr := s.stripeClient.UpdateCustomer(ctx, stripeclient.UpdateCustomerInput{
			CustomerID:       customerID,
			OrganizationID:   authCtx.ActiveOrganizationID,
			OrganizationSlug: authCtx.OrganizationSlug,
			OrganizationName: identity.name,
			Email:            identity.email,
			AccountType:      identity.accountType,
		}); updateErr != nil {
			s.logger.WarnContext(ctx, "failed to refresh Stripe customer identity", attr.SlogError(updateErr))
		}
	} else {
		customer, err := s.stripeClient.CreateCustomer(ctx, stripeclient.CreateCustomerInput{
			OrganizationID:   authCtx.ActiveOrganizationID,
			OrganizationSlug: authCtx.OrganizationSlug,
			OrganizationName: identity.name,
			Email:            identity.email,
			AccountType:      identity.accountType,
			IdempotencyKey:   fmt.Sprintf("customer:%s", authCtx.ActiveOrganizationID),
		})
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to create Stripe customer").LogError(ctx, s.logger)
		}
		customerID = customer.ID
	}
	billingURL := s.siteURL.JoinPath(authCtx.OrganizationSlug, "billing").String()
	replaceLifecycleIntentKey, err := s.expireLifecycleStaleCheckoutSession(ctx, billingMetadata, customerID, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug, billingURL, identity, proposedIntent, now)
	if err != nil {
		return "", err
	}
	replaceExpiredSessionID, err := s.expiredCheckoutSessionForReplacement(ctx, billingMetadata, customerID, now)
	if err != nil {
		return "", err
	}

	preparedIntent, err := s.prepareStripeCheckoutIntent(ctx, authCtx.ActiveOrganizationID, customerID, now, proposedIntent, replaceExpiredSessionID, replaceLifecycleIntentKey)
	if err != nil {
		return "", err
	}

	checkout, err := s.stripeClient.CreateCheckoutSession(ctx, stripeclient.CreateCheckoutSessionInput{
		CustomerID:         preparedIntent.customerID,
		OrganizationID:     authCtx.ActiveOrganizationID,
		OrganizationSlug:   authCtx.OrganizationSlug,
		OrganizationName:   identity.name,
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

	convertedTrial, err := s.convertEnterpriseTrialForCheckoutTx(ctx, dbtx, authCtx.ActiveOrganizationID, expectedTrial, checkoutIntentTrialFingerprint(preparedIntent.idempotencyKey), checkout.ID)
	if err != nil {
		if errors.Is(err, errStripeCheckoutTrialLifecycleChanged) {
			return "", oops.E(oops.CodeConflict, err, "trial lifecycle changed while Stripe Checkout was being created").LogWarn(ctx, s.logger)
		}
		return "", oops.E(oops.CodeUnexpected, err, "failed to convert enterprise trial during Stripe Checkout").LogError(ctx, s.logger)
	}

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

	if convertedTrial {
		if s.productFeatures != nil {
			for _, runtimeFeature := range productfeatures.TrialRuntimeFeatures {
				s.productFeatures.UpdateFeatureCache(ctx, authCtx.ActiveOrganizationID, runtimeFeature, true)
			}
		}
		if s.trial != nil {
			if err := s.trial.TrialInactive(ctx, authCtx.ActiveOrganizationID); err != nil {
				s.logger.WarnContext(ctx, "failed to stop enterprise trial notifications after Stripe Checkout conversion")
			}
		}
		if provisioner, ok := s.openRouter.(checkoutTrialProvisioner); ok {
			for _, keyType := range openrouter.AllKeyTypes {
				if err := provisioner.ReconcileAPIKeyDisabled(ctx, authCtx.ActiveOrganizationID, keyType); err != nil {
					s.logger.WarnContext(ctx, "failed to reconcile model provider key after Stripe Checkout conversion")
				}
			}
		}
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
	replaceLifecycleIntentKey pgtype.Text,
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
		if isStripeCheckoutCASConflict(err) {
			return preparedStripeCheckoutIntent{}, oops.E(oops.CodeConflict, err, "billing state changed while Checkout was being prepared").LogWarn(ctx, s.logger)
		}
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
		TrialFingerprint:                 checkoutIntentTrialFingerprint(proposed.idempotencyKey),
		OrganizationID:                   organizationID,
		StripeCustomerID:                 customerID,
		ReplaceExpiredSessionID:          replaceExpiredSessionID,
		ReplaceLifecycleIntentKey:        replaceLifecycleIntentKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isStripeCheckoutCASConflict(err) {
			return preparedStripeCheckoutIntent{}, oops.E(oops.CodeConflict, err, "billing state changed while Checkout was being prepared").LogWarn(ctx, s.logger)
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

func isStripeCheckoutCASConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == pgerrcode.DeadlockDetected || pgErr.Code == pgerrcode.SerializationFailure)
}

func (s *Service) checkoutNow() time.Time {
	if s.now == nil {
		return time.Now().UTC().Truncate(time.Second)
	}
	return s.now().UTC().Truncate(time.Second)
}

func (s *Service) expireLifecycleStaleCheckoutSession(
	ctx context.Context,
	metadata repo.BillingMetadatum,
	customerID, organizationID, organizationSlug, billingURL string,
	identity stripeOrganizationIdentity,
	proposed stripeCheckoutIntent,
	now time.Time,
) (pgtype.Text, error) {
	if !metadata.StripeCheckoutIdempotencyKey.Valid || !metadata.StripeCheckoutExpiresAt.Valid ||
		!metadata.StripeCheckoutExpiresAt.Time.After(now) ||
		checkoutIntentTrialFingerprint(metadata.StripeCheckoutIdempotencyKey.String) == checkoutIntentTrialFingerprint(proposed.idempotencyKey) {
		return pgtype.Text{String: "", Valid: false}, nil
	}

	staleIntent, err := checkoutIntentFromMetadata(metadata)
	if err != nil {
		return pgtype.Text{}, oops.E(oops.CodeUnavailable, err, "failed to recover the previous Stripe Checkout intent").LogWarn(ctx, s.logger)
	}
	sessionID := ""
	if metadata.StripeCheckoutSessionID.Valid {
		sessionID = metadata.StripeCheckoutSessionID.String
	} else {
		// Same idempotency key as the original request, so the input must match it exactly.
		stale, createErr := s.stripeClient.CreateCheckoutSession(ctx, stripeclient.CreateCheckoutSessionInput{
			CustomerID: customerID, OrganizationID: organizationID, OrganizationSlug: organizationSlug,
			OrganizationName: identity.name,
			SuccessURL:       billingURL, CancelURL: billingURL, TrialEnd: staleIntent.trialEnd,
			BillingCycleAnchor: staleIntent.billingCycleAnchor, ExpiresAt: staleIntent.expiresAt, IdempotencyKey: staleIntent.idempotencyKey,
		})
		if createErr != nil || stale == nil || stale.ID == "" {
			return pgtype.Text{}, oops.E(oops.CodeUnavailable, createErr, "failed to recover the previous Stripe Checkout session").LogWarn(ctx, s.logger)
		}
		sessionID = stale.ID
	}
	expirer, ok := s.stripeClient.(stripeCheckoutSessionExpirer)
	if !ok {
		return pgtype.Text{}, oops.E(oops.CodeUnavailable, nil, "Stripe Checkout expiration is unavailable").LogWarn(ctx, s.logger)
	}
	if err := expirer.ExpireCheckoutSession(ctx, sessionID); err != nil {
		state, retrieveErr := s.stripeClient.GetCheckoutSession(ctx, sessionID)
		if retrieveErr != nil || state == nil || state.ID != sessionID || state.CustomerID != customerID || state.Status != "expired" || state.SubscriptionID != "" {
			return pgtype.Text{}, oops.E(oops.CodeUnavailable, err, "failed to expire the previous Stripe Checkout session").LogWarn(ctx, s.logger)
		}
	}
	return pgtype.Text{String: staleIntent.idempotencyKey, Valid: true}, nil
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

func checkoutIntentFromMetadata(metadata repo.BillingMetadatum) (stripeCheckoutIntent, error) {
	return checkoutIntentFromFields(metadata.StripeCheckoutIdempotencyKey, metadata.StripeCheckoutBillingCycleAnchor, metadata.StripeCheckoutTrialEnd, metadata.StripeCheckoutExpiresAt)
}

func checkoutIntentFromRow(row repo.PrepareStripeCheckoutIntentRow) (stripeCheckoutIntent, error) {
	return checkoutIntentFromFields(row.StripeCheckoutIdempotencyKey, row.StripeCheckoutBillingCycleAnchor, row.StripeCheckoutTrialEnd, row.StripeCheckoutExpiresAt)
}

func checkoutIntentFromFields(idempotencyKey pgtype.Text, billingCycleAnchor, storedTrialEnd, expiresAt pgtype.Timestamptz) (stripeCheckoutIntent, error) {
	if !idempotencyKey.Valid || !billingCycleAnchor.Valid || !expiresAt.Valid {
		return stripeCheckoutIntent{}, errors.New("required Checkout intent field is null")
	}

	var trialEnd *time.Time
	if storedTrialEnd.Valid {
		value := storedTrialEnd.Time.UTC()
		trialEnd = &value
	}

	return stripeCheckoutIntent{
		idempotencyKey:     idempotencyKey.String,
		billingCycleAnchor: billingCycleAnchor.Time.UTC(),
		trialEnd:           trialEnd,
		expiresAt:          expiresAt.Time.UTC(),
	}, nil
}

func newStripeCheckoutIntent(organizationID string, now time.Time, productTrialEnd *time.Time) stripeCheckoutIntent {
	return newStripeCheckoutIntentForTrial(organizationID, now, productTrialEnd, nil)
}

func newStripeCheckoutIntentForTrial(organizationID string, now time.Time, productTrialEnd *time.Time, trial *stripeCheckoutTrialFingerprint) stripeCheckoutIntent {
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
		idempotencyKey:     fmt.Sprintf("checkout-session:%s:%d:%d:%s", organizationID, anchor.Unix(), expiresAt.Unix(), stripeCheckoutTrialFingerprintDigest(trial)),
		billingCycleAnchor: anchor,
		trialEnd:           stripeTrialEnd,
		expiresAt:          expiresAt,
	}
}

func newStripeCheckoutTrialFingerprint(trial trialsrepo.Trial) *stripeCheckoutTrialFingerprint {
	return &stripeCheckoutTrialFingerprint{
		organizationID: trial.OrganizationID, tier: trial.Tier, endsAt: trial.EndsAt.Time.UTC(),
		convertedAt: checkoutOptionalTime(trial.ConvertedAt), demotedAt: checkoutOptionalTime(trial.DemotedAt),
		createdAt: trial.CreatedAt.Time.UTC(), updatedAt: trial.UpdatedAt.Time.UTC(),
	}
}

func stripeCheckoutTrialFingerprintDigest(trial *stripeCheckoutTrialFingerprint) string {
	if trial == nil {
		return "none"
	}
	payload := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", trial.organizationID, trial.tier, trial.endsAt.Format(time.RFC3339Nano), checkoutOptionalTimeString(trial.convertedAt), checkoutOptionalTimeString(trial.demotedAt), trial.createdAt.Format(time.RFC3339Nano), trial.updatedAt.Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum[:12])
}

func checkoutIntentTrialFingerprint(idempotencyKey string) string {
	for index := len(idempotencyKey) - 1; index >= 0; index-- {
		if idempotencyKey[index] == ':' {
			return idempotencyKey[index+1:]
		}
	}
	return ""
}

func checkoutOptionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func checkoutOptionalTimeString(value *time.Time) string {
	if value == nil {
		return "null"
	}
	return value.UTC().Format(time.RFC3339Nano)
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
