package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
	logger := s.logger.With(attr.SlogOrganizationID(payload.ID))
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin enterprise trial conversion transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	trial, err := trialsRepo.New(tx).LockTrialLifecycle(ctx, payload.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_ = tx.Rollback(ctx)
		return nil, s.rejectTrialChange(ctx, logger, payload.ID, "look up organization without enterprise trial", "organization has no enterprise trial to convert")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock enterprise trial conversion lifecycle").LogError(ctx, logger)
	}

	if trial.Tier != "enterprise" {
		_ = tx.Rollback(ctx)
		return nil, oops.E(oops.CodeConflict, nil, "organization has no enterprise trial to convert")
	}

	// The lifecycle row is always first, followed by every key advisory lock in
	// canonical order, before any organization or key row is read.
	for _, keyType := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, tx, payload.ID, keyType); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock openrouter %s key for enterprise trial conversion", keyType).LogError(ctx, logger)
		}
	}

	organization, err := repo.New(tx).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: payload.ID, AllowSlug: false})
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
		s.updateTrialFeatureCache(ctx, payload.ID)
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
	for _, change := range keyChanges {
		beforeKeys = append(beforeKeys, conversionKeyAuditSnapshot(change.Before))
		afterKeys = append(afterKeys, conversionKeyAuditSnapshot(change.After))
	}
	before := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: organization.AccountType, Whitelisted: organization.Whitelisted, Disabled: organization.DisabledAt.Valid},
		Trial:        conversionTrialAuditSnapshot(trial.Tier, trial.EndsAt, trial.ConvertedAt, trial.DemotedAt), Keys: beforeKeys,
	}
	after := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: "enterprise", Whitelisted: true, Disabled: organization.DisabledAt.Valid},
		Trial:        conversionTrialAuditSnapshot(convertedTrial.Tier, convertedTrial.EndsAt, convertedTrial.ConvertedAt, convertedTrial.DemotedAt), Keys: afterKeys,
	}
	actor, actorDisplayName := enterpriseTrialConversionAuditActor(ctx)
	if err := s.audit.LogOrganizationEnterpriseTrialConverted(ctx, tx, audit.LogOrganizationEnterpriseTrialConvertedEvent{
		OrganizationID: payload.ID, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil, Before: before, After: after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log enterprise trial conversion").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit enterprise trial conversion").LogError(ctx, logger)
	}

	s.updateTrialFeatureCache(ctx, payload.ID)
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
		if err := s.openRouter.ReconcileAPIKeyDisabled(ctx, organizationID, keyType); err != nil {
			return oops.E(oops.CodeUnexpected, err, "reconcile openrouter %s key after enterprise trial conversion", keyType).LogError(ctx, s.logger)
		}
	}
	return nil
}

func conversionTrialAuditSnapshot(tier string, endsAt, convertedAt, demotedAt pgtype.Timestamptz) audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot {
	return audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot{Tier: tier, EndsAt: pgTimePtr(endsAt), ConvertedAt: pgTimePtr(convertedAt), DemotedAt: pgTimePtr(demotedAt)}
}

func conversionKeyAuditSnapshot(state openrouter.EnterpriseTrialConversionKeyState) audit.OrganizationEnterpriseTrialConversionKeySnapshot {
	return audit.OrganizationEnterpriseTrialConversionKeySnapshot{KeyType: string(state.KeyType), DisableCauses: state.DisableCauses, StoredDisabled: state.Disabled, EffectiveDisabled: openrouter.EffectiveDisabled(state.Disabled, state.DisableCauses), MonthlyCredits: state.MonthlyCredits}
}

func enterpriseTrialConversionResult(organizationID string, convertedAt pgtype.Timestamptz) *gen.MarkEnterpriseTrialConvertedResult {
	return &gen.MarkEnterpriseTrialConvertedResult{OrganizationID: organizationID, ConvertedAt: convertedAt.Time.Format(time.RFC3339)}
}

func enterpriseTrialConversionAuditActor(ctx context.Context) (urn.Principal, *string) {
	label := "Platform administrator"
	authCtx, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == "" {
		return urn.NewPrincipal(urn.PrincipalTypeUser, "system"), &label
	}
	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.SessionID), &label
}

func pgTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
