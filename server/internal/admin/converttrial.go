package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// MarkEnterpriseTrialConverted owns the eligibility and idempotency boundary
// for manual enterprise conversion. The business transition intentionally stays
// disabled until its lifecycle, organization, key, audit, and outbox writes can
// commit atomically.
func (s *Service) MarkEnterpriseTrialConverted(ctx context.Context, payload *gen.MarkEnterpriseTrialConvertedPayload) (*gen.AdminOrganization, error) {
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
		return adminOrganizationFromGetRow(organization), nil
	}

	demotedAccess := trial.DemotedAt.Valid && organization.AccountType == "free" && !organization.Whitelisted
	runningAccess := !trial.DemotedAt.Valid && organization.AccountType == "enterprise" && organization.Whitelisted
	if !demotedAccess && !runningAccess {
		_ = tx.Rollback(ctx)
		return nil, oops.E(oops.CodeConflict, nil, "enterprise trial has incompatible organization access")
	}

	_ = tx.Rollback(ctx)
	return nil, oops.E(oops.CodeUnexpected, nil, "enterprise trial conversion business transition is not implemented")
}
