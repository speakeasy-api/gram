package admin

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func (s *Service) MarkEnterpriseTrialConverted(ctx context.Context, payload *gen.MarkEnterpriseTrialConvertedPayload) (*gen.AdminOrganization, error) {
	logger := s.logger.With(attr.SlogOrganizationID(payload.ID))
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin enterprise trial conversion transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	trials := trialsRepo.New(tx)
	trial, err := trials.LockEnterpriseTrialForConversion(ctx, payload.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_ = tx.Rollback(ctx)
		return nil, s.rejectTrialChange(ctx, logger, payload.ID,
			"look up organization after missing enterprise trial",
			"organization has no enterprise trial to convert")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock enterprise trial for conversion").LogError(ctx, logger)
	}

	organization, err := repo.New(tx).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: payload.ID, AllowSlug: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization for enterprise trial conversion").LogError(ctx, logger)
	}
	if organization.AccountType == string(billing.TierPayg) {
		return nil, oops.E(oops.CodeConflict, nil, "PAYG organizations cannot be converted through the enterprise trial endpoint")
	}

	if trial.ConvertedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "commit idempotent enterprise trial conversion").LogError(ctx, logger)
		}
		s.notifyTrialInactive(ctx, logger, payload.ID)
		return s.readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after enterprise trial conversion")
	}

	lockedKeys := make(map[openrouter.KeyType]*pgxpool.Conn, len(openrouter.AllKeyTypes))
	var result *gen.AdminOrganization
	err = s.withTrialKeyBillingLocks(ctx, logger, payload.ID, openrouter.AllKeyTypes, lockedKeys, func() error {
		var lockedErr error
		result, lockedErr = s.convertEnterpriseTrialLocked(ctx, logger, payload.ID, lockedKeys, tx, trials, trial.DemotedAt.Valid, organization)
		return lockedErr
	})
	if err == nil {
		return result, nil
	}

	var shareable *oops.ShareableError
	if errors.As(err, &shareable) {
		return nil, shareable
	}
	if errors.Is(err, keybillinglock.ErrAcquireTimeout) {
		return nil, oops.E(oops.CodeUnavailable, err, "another billing operation is in progress; retry shortly").LogWarn(ctx, logger)
	}
	return nil, oops.E(oops.CodeUnexpected, err, "lock inference keys for enterprise trial conversion").LogError(ctx, logger)
}

func (s *Service) convertEnterpriseTrialLocked(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	lockedKeys map[openrouter.KeyType]*pgxpool.Conn,
	tx pgx.Tx,
	trials *trialsRepo.Queries,
	wasDemoted bool,
	organization repo.AdminGetOrganizationRow,
) (*gen.AdminOrganization, error) {
	legacyZeroLimitKeys, err := s.convertEnterpriseTrialKeys(ctx, logger, lockedKeys, organizationID)
	if err != nil {
		return nil, err
	}

	if wasDemoted {
		restored, err := trials.RestoreOrganizationFromTrial(ctx, trialsRepo.RestoreOrganizationFromTrialParams{
			OrganizationID: organizationID,
			AccountType:    string(billing.TierEnterprise),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "restore organization from converted enterprise trial").LogError(ctx, logger)
		}
		organization.Name = restored.Name
		organization.Slug = restored.Slug
		if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, organizationID, true); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "restore converted enterprise trial runtime features").LogError(ctx, logger)
		}
	}

	if _, err := trials.MarkEnterpriseTrialConverted(ctx, organizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mark enterprise trial converted").LogError(ctx, logger)
	}

	actor, actorDisplayName, _ := adminActor(ctx)
	if err := s.audit.LogOrganizationEnterpriseTrialConverted(ctx, tx, audit.LogOrganizationEnterpriseTrialConvertedEvent{
		OrganizationID:   organizationID,
		Actor:            actor,
		ActorDisplayName: actorDisplayName,
		ActorSlug:        nil,
		OrganizationName: organization.Name,
		OrganizationSlug: organization.Slug,
		ConversionSource: "admin",
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log enterprise trial conversion").LogError(ctx, logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit enterprise trial conversion").LogError(ctx, logger)
	}

	s.recapRevivedKeys(ctx, logger, lockedKeys, organizationID, legacyZeroLimitKeys)
	if wasDemoted {
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			s.productFeatures.UpdateFeatureCache(ctx, organizationID, feature, true)
		}
	}
	s.notifyTrialInactive(ctx, logger, organizationID)

	return s.readOrganizationAfterWrite(ctx, organizationID, "fetch organization after enterprise trial conversion")
}

func (s *Service) convertEnterpriseTrialKeys(ctx context.Context, logger *slog.Logger, lockedKeys map[openrouter.KeyType]*pgxpool.Conn, organizationID string) ([]openrouter.KeyType, error) {
	enterpriseLimit, ok := openrouter.AccountTypeCreditLimit(billing.TierEnterprise)
	if !ok {
		return nil, oops.E(oops.CodeUnexpected, nil, "enterprise openrouter credit policy is missing").LogError(ctx, logger)
	}

	var legacyZeroLimitKeys []openrouter.KeyType
	for _, keyType := range openrouter.AllKeyTypes {
		conn := lockedKeys[keyType]
		if conn == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "missing openrouter %s key lock", keyType).LogError(ctx, logger)
		}
		row, err := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: organizationID, KeyType: string(keyType)})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "read openrouter %s key", keyType).LogError(ctx, logger)
		}

		if row.MonthlyCredits == 0 {
			legacyZeroLimitKeys = append(legacyZeroLimitKeys, keyType)
		}
		if slices.Contains(row.DisableCauses, string(openrouter.DisableCauseTrialDemotion)) {
			_, _, err = s.openRouter.RemoveAPIKeyDisableCauseWithDB(ctx, conn, organizationID, keyType, openrouter.DisableCauseTrialDemotion, &enterpriseLimit)
		} else {
			_, err = s.openRouter.RefreshAPIKeyLimitWithDB(ctx, conn, organizationID, keyType, &enterpriseLimit)
		}
		switch {
		case errors.Is(err, ErrOpenRouterUnavailable):
			return nil, oops.E(oops.CodeInvalid, err, "this server cannot convert enterprise trials: it is missing either the OpenRouter provisioning key or a usable encryption key. The server log says which at startup")
		case err != nil:
			return nil, oops.E(oops.CodeGatewayError, err, "apply enterprise limit to openrouter %s key", keyType).LogError(ctx, logger)
		}
	}

	return legacyZeroLimitKeys, nil
}

func (s *Service) notifyTrialInactive(ctx context.Context, logger *slog.Logger, organizationID string) {
	if err := s.trial.TrialInactive(ctx, organizationID); err != nil {
		logger.ErrorContext(ctx, "failed to notify trial inactive", attr.SlogError(err))
	}
}
