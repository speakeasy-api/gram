package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	enterprisetrialsrepo "github.com/speakeasy-api/gram/server/internal/enterprisetrials/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type DemoteExpiredEnterpriseTrials struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	repo       *enterprisetrialsrepo.Queries
	openRouter openrouter.Provisioner
	audit      *audit.Logger
}

func NewDemoteExpiredEnterpriseTrials(
	logger *slog.Logger,
	db *pgxpool.Pool,
	openRouterProvisioner openrouter.Provisioner,
	auditLogger *audit.Logger,
) *DemoteExpiredEnterpriseTrials {
	return &DemoteExpiredEnterpriseTrials{
		logger:     logger.With(attr.SlogComponent("demote_expired_enterprise_trials")),
		db:         db,
		repo:       enterprisetrialsrepo.New(db),
		openRouter: openRouterProvisioner,
		audit:      auditLogger,
	}
}

func (d *DemoteExpiredEnterpriseTrials) List(ctx context.Context) ([]string, error) {
	organizationIDs, err := d.repo.ListExpiredEnterpriseTrials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list expired enterprise trials: %w", err)
	}

	return organizationIDs, nil
}

type DemoteExpiredEnterpriseTrialArgs struct {
	OrganizationID string
}

func (d *DemoteExpiredEnterpriseTrials) Demote(ctx context.Context, args DemoteExpiredEnterpriseTrialArgs) error {
	dbtx, err := d.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin enterprise trial demotion: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := enterprisetrialsrepo.New(dbtx)

	trial, err := tx.MarkEnterpriseTrialDemoted(ctx, args.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// A conversion or a manual reinstatement landed between the list and
		// this write. The organization is no longer ours to demote.
		d.logger.InfoContext(ctx, "expired enterprise trial changed before demotion",
			attr.SlogOrganizationID(args.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("mark enterprise trial demoted: %w", err)
	}

	// The stamp above holds a row lock until this transaction ends, so a
	// conversion cannot land while the keys go down. A failure here rolls the
	// stamp back and leaves the trial armed for the next sweep, which a lockdown
	// after the commit would not get: a stamped row drops out of the sweep.
	for _, keyType := range openrouter.AllKeyTypes {
		if err := d.openRouter.DisableAPIKey(ctx, args.OrganizationID, keyType); err != nil {
			return fmt.Errorf("disable openrouter %s key: %w", keyType, err)
		}
	}

	organization, err := tx.DemoteOrganizationToFree(ctx, args.OrganizationID)
	if err != nil {
		return fmt.Errorf("demote organization to free: %w", err)
	}

	if err := d.audit.LogOrganizationEnterpriseTrialDemoted(ctx, dbtx, audit.LogOrganizationEnterpriseTrialDemotedEvent{
		OrganizationID:      args.OrganizationID,
		Actor:               urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		ActorDisplayName:    nil,
		ActorSlug:           nil,
		OrganizationName:    organization.Name,
		OrganizationSlug:    organization.Slug,
		PreviousAccountType: organization.PreviousAccountType,
		TrialEndsAt:         trial.EndsAt.Time,
	}); err != nil {
		return fmt.Errorf("log enterprise trial demotion: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit enterprise trial demotion: %w", err)
	}

	return nil
}
