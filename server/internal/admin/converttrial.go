package admin

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MarkEnterpriseTrialConverted atomically records an enterprise contract and
// its complete local access policy before reconciling provider state.
func (s *Service) MarkEnterpriseTrialConverted(ctx context.Context, payload *gen.MarkEnterpriseTrialConvertedPayload) (*gen.MarkEnterpriseTrialConvertedResult, error) {
	return s.markEnterpriseTrialConverted(ctx, payload.ID)
}

// markEnterpriseTrialConverted is the dedicated operation's transaction
// coordinator. Generic organization updates must never call it.
func (s *Service) markEnterpriseTrialConverted(ctx context.Context, organizationID string) (*gen.MarkEnterpriseTrialConvertedResult, error) {
	payload := &gen.MarkEnterpriseTrialConvertedPayload{ID: organizationID, AdminSessionToken: nil}
	logger := s.logger.With(attr.SlogOrganizationID(payload.ID))
	lockConn, releaseFeatureLocks, err := s.productFeatures.AcquireFeatureCacheLocks(ctx, payload.ID, productfeatures.TrialRuntimeFeatures)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock enterprise runtime features").LogError(ctx, logger)
	}
	featureLocksReleased := false
	releaseFeatures := func() {
		if !featureLocksReleased {
			releaseFeatureLocks()
			featureLocksReleased = true
		}
	}
	defer releaseFeatures()
	refreshFeatureCache := func() error {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			if cacheErr := s.productFeatures.UpdateFeatureCacheUnderLock(cacheCtx, lockConn, payload.ID, feature); cacheErr != nil {
				return fmt.Errorf("refresh %s cache: %w", feature, cacheErr)
			}
		}
		return nil
	}

	tx, err := lockConn.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin enterprise trial conversion transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	trial, err := trialsRepo.New(tx).LockTrialLifecycle(ctx, payload.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_ = tx.Rollback(ctx)
		releaseFeatures()
		return nil, s.rejectTrialChange(ctx, logger, payload.ID, "look up organization without enterprise trial", "organization has no enterprise trial to convert")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock enterprise trial conversion lifecycle").LogError(ctx, logger)
	}

	// Inside the feature-lock envelope, the lifecycle row is first, followed
	// by every billing lock and then every provisioning lock in canonical key
	// order, before any organization or key row is read.
	for _, keyType := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, tx, payload.ID, keyType); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock openrouter %s key for enterprise trial conversion", keyType).LogError(ctx, logger)
		}
	}
	for _, keyType := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyProvisioningTransactionLock(ctx, tx, payload.ID, keyType); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock openrouter %s key provisioning for enterprise trial conversion", keyType).LogError(ctx, logger)
		}
	}

	queries := repo.New(tx)
	if _, err := queries.LockOrganizationMetadata(ctx, payload.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "organization not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization metadata for enterprise trial conversion").LogError(ctx, logger)
	}

	if trial.Tier != "enterprise" {
		_ = tx.Rollback(ctx)
		return nil, oops.E(oops.CodeConflict, nil, "organization has no enterprise trial to convert")
	}

	organization, err := queries.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: payload.ID, AllowSlug: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization for enterprise trial conversion").LogError(ctx, logger)
	}

	if trial.ConvertedAt.Valid {
		if organization.AccountType != "enterprise" || !organization.Whitelisted {
			_ = tx.Rollback(ctx)
			return nil, oops.E(oops.CodeConflict, nil, "converted enterprise trial has incompatible organization access")
		}

		if err := tx.Rollback(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "close enterprise trial conversion retry transaction").LogError(ctx, logger)
		}
		if err := refreshFeatureCache(); err != nil {
			releaseFeatures()
			return nil, oops.E(oops.CodeUnexpected, err, "refresh enterprise runtime feature cache after conversion retry").LogError(ctx, logger)
		}
		releaseFeatures()
		if err := s.trial.TrialInactive(ctx, payload.ID); err != nil {
			logger.WarnContext(ctx, "failed to stop enterprise trial notifications on conversion retry", attr.SlogError(err))
		}
		if err := s.reconcileConvertedTrialKeys(ctx, payload.ID); err != nil {
			return nil, err
		}
		return enterpriseTrialConversionResult(payload.ID, trial.ConvertedAt), nil
	}

	demotedAccess := organization.AccountType == "free" && !organization.Whitelisted
	runningAccess := organization.AccountType == "enterprise" && organization.Whitelisted
	if !demotedAccess && !runningAccess {
		_ = tx.Rollback(ctx)
		return nil, oops.E(oops.CodeConflict, nil, "enterprise trial has incompatible organization access")
	}

	enterpriseFloor, ok := openrouter.DefaultCreditLimit(payload.ID, billing.TierEnterprise, false)
	if !ok || enterpriseFloor <= 0 {
		return nil, oops.E(oops.CodeUnexpected, nil, "enterprise tier has no OpenRouter credit policy").LogError(ctx, logger)
	}

	keyChanges := make([]openrouter.EnterpriseTrialConversionKeyChange, 0, len(openrouter.AllKeyTypes))
	for _, keyType := range openrouter.AllKeyTypes {
		change, prepareErr := s.openRouter.PrepareEnterpriseTrialConversionKeyWithDB(ctx, tx, payload.ID, keyType, int64(enterpriseFloor))
		switch {
		case errors.Is(prepareErr, ErrOpenRouterUnavailable):
			return nil, oops.E(oops.CodeInvalid, prepareErr, "this server cannot update model provider key lifecycle state")
		case prepareErr != nil:
			return nil, oops.E(oops.CodeUnexpected, prepareErr, "prepare openrouter %s key for enterprise trial conversion", keyType).LogError(ctx, logger)
		}
		if change.Exists {
			keyChanges = append(keyChanges, change)
		}
	}

	rows, err := trialsRepo.New(tx).MarkTrialConverted(ctx, payload.ID)
	if err != nil || rows != 1 {
		return nil, oops.E(oops.CodeUnexpected, err, "mark enterprise trial converted").LogError(ctx, logger)
	}
	_, err = trialsRepo.New(tx).RestoreOrganizationFromTrial(ctx, trialsRepo.RestoreOrganizationFromTrialParams{OrganizationID: payload.ID, AccountType: "enterprise"})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "restore organization after enterprise trial conversion").LogError(ctx, logger)
	}
	if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, payload.ID, true); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "restore enterprise runtime features").LogError(ctx, logger)
	}
	convertedTrial, err := trialsRepo.New(tx).GetTrial(ctx, payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read converted enterprise trial").LogError(ctx, logger)
	}

	beforeKeys := make([]audit.OrganizationEnterpriseTrialConversionKeySnapshot, 0, len(keyChanges))
	afterKeys := make([]audit.OrganizationEnterpriseTrialConversionKeySnapshot, 0, len(keyChanges))
	keyAccessChanged := false
	for _, change := range keyChanges {
		accessChanged := openrouter.EffectiveDisabled(change.Before.Disabled, change.Before.DisableCauses) != openrouter.EffectiveDisabled(change.After.Disabled, change.After.DisableCauses)
		keyAccessChanged = keyAccessChanged || accessChanged
		beforeKeys = append(beforeKeys, conversionKeyAuditSnapshot(change.Before, accessChanged))
		afterKeys = append(afterKeys, conversionKeyAuditSnapshot(change.After, accessChanged))
	}
	snapshotTime := time.Now().UTC()
	before := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: organization.AccountType, Whitelisted: organization.Whitelisted, Disabled: organization.DisabledAt.Valid},
		Trial:        conversionTrialAuditSnapshot(trial.Tier, trial.EndsAt, trial.ConvertedAt, trial.DemotedAt, snapshotTime), Keys: beforeKeys,
	}
	after := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: "enterprise", Whitelisted: true, Disabled: organization.DisabledAt.Valid},
		Trial:        conversionTrialAuditSnapshot(convertedTrial.Tier, convertedTrial.EndsAt, convertedTrial.ConvertedAt, convertedTrial.DemotedAt, snapshotTime), Keys: afterKeys,
	}
	actor, actorDisplayName := enterpriseTrialConversionAuditActor(ctx)
	if err := s.audit.LogOrganizationEnterpriseTrialConverted(ctx, tx, audit.LogOrganizationEnterpriseTrialConvertedEvent{
		OrganizationID: payload.ID, ConversionSource: "admin", KeyAccessChanged: &keyAccessChanged, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil, Before: before, After: after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log enterprise trial conversion").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit enterprise trial conversion").LogError(ctx, logger)
	}

	if err := refreshFeatureCache(); err != nil {
		releaseFeatures()
		return nil, oops.E(oops.CodeUnexpected, err, "refresh enterprise runtime feature cache after conversion").LogError(ctx, logger)
	}
	releaseFeatures()
	if err := s.trial.TrialInactive(ctx, payload.ID); err != nil {
		logger.WarnContext(ctx, "failed to stop enterprise trial notifications after conversion", attr.SlogError(err))
	}
	if err := s.reconcileConvertedTrialKeys(ctx, payload.ID); err != nil {
		return nil, err
	}
	return enterpriseTrialConversionResult(payload.ID, convertedTrial.ConvertedAt), nil
}

func (s *Service) reconcileConvertedTrialKeys(ctx context.Context, organizationID string) error {
	for _, keyType := range openrouter.AllKeyTypes {
		if err := s.openRouter.ReconcileAPIKeyConversionPolicy(ctx, organizationID, keyType); err != nil {
			return oops.E(oops.CodeUnexpected, err, "reconcile openrouter %s key after enterprise trial conversion", keyType).LogError(ctx, s.logger)
		}
	}
	return nil
}

func conversionTrialAuditSnapshot(tier string, endsAt, convertedAt, demotedAt pgtype.Timestamptz, now time.Time) audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot {
	status := "running"
	switch {
	case convertedAt.Valid:
		status = "converted"
	case demotedAt.Valid:
		status = "demoted"
	case !endsAt.Time.After(now):
		status = "expired"
	case !endsAt.Time.After(now.Add(7 * 24 * time.Hour)):
		status = "ending_soon"
	}
	return audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot{Status: status, Tier: tier, EndsAt: pgTimePtr(endsAt), ConvertedAt: pgTimePtr(convertedAt), DemotedAt: pgTimePtr(demotedAt)}
}

func conversionKeyAuditSnapshot(state openrouter.EnterpriseTrialConversionKeyState, accessChanged bool) audit.OrganizationEnterpriseTrialConversionKeySnapshot {
	return audit.OrganizationEnterpriseTrialConversionKeySnapshot{KeyType: string(state.KeyType), StoredDisabled: state.Disabled, EffectiveDisabled: openrouter.EffectiveDisabled(state.Disabled, state.DisableCauses), KeyAccessChanged: accessChanged, MonthlyCredits: state.MonthlyCredits}
}

func enterpriseTrialConversionResult(organizationID string, convertedAt pgtype.Timestamptz) *gen.MarkEnterpriseTrialConvertedResult {
	return &gen.MarkEnterpriseTrialConvertedResult{OrganizationID: organizationID, ConvertedAt: convertedAt.Time.Format(time.RFC3339)}
}

func enterpriseTrialConversionAuditActor(ctx context.Context) (urn.Principal, *string) {
	actor, displayName, operatorEmail := adminActor(ctx)
	if displayName == nil {
		return actor, nil
	}

	normalized := strings.TrimSpace(*displayName)
	if operatorEmail != nil && strings.EqualFold(normalized, strings.TrimSpace(*operatorEmail)) {
		return actor, nil
	}
	if address, err := mail.ParseAddress(normalized); err == nil && strings.Contains(address.Address, "@") {
		return actor, nil
	}
	return actor, &normalized
}

func pgTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
