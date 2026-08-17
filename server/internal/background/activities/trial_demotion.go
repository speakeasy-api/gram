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
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type DemoteExpiredTrials struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	repo       *trialsrepo.Queries
	openRouter openrouter.Provisioner
	audit      *audit.Logger
	trial      trialemails.Notifier
}

func NewDemoteExpiredTrials(
	logger *slog.Logger,
	db *pgxpool.Pool,
	openRouterProvisioner openrouter.Provisioner,
	auditLogger *audit.Logger,
	trialNotifier trialemails.Notifier,
) *DemoteExpiredTrials {
	if trialNotifier == nil {
		trialNotifier = trialemails.NoopNotifier{}
	}

	return &DemoteExpiredTrials{
		logger:     logger.With(attr.SlogComponent("demote_expired_trials")),
		db:         db,
		repo:       trialsrepo.New(db),
		openRouter: openRouterProvisioner,
		audit:      auditLogger,
		trial:      trialNotifier,
	}
}

func (d *DemoteExpiredTrials) List(ctx context.Context) ([]string, error) {
	organizationIDs, err := d.repo.ListExpiredTrials(ctx)
	if err != nil {
		return nil, fmt.Errorf("query trials due for demotion: %w", err)
	}

	return organizationIDs, nil
}

type DemoteExpiredTrialArgs struct {
	OrganizationID string
}

func (d *DemoteExpiredTrials) Demote(ctx context.Context, args DemoteExpiredTrialArgs) error {
	dbtx, err := d.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin trial demotion: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := trialsrepo.New(dbtx)

	trial, err := tx.MarkTrialDemoted(ctx, args.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// A conversion, a completed demotion, or a re-arm (ends_at moved
		// forward, stamps cleared) landed between the list and this write.
		// Only a closed trial should drop out of the Loops sequence.
		d.logger.InfoContext(ctx, "expired trial changed before demotion",
			attr.SlogOrganizationID(args.OrganizationID))
		if err := d.notifyTrialInactiveIfClosed(ctx, args.OrganizationID); err != nil {
			return err
		}
		return nil
	case err != nil:
		return fmt.Errorf("mark trial demoted: %w", err)
	}

	// The stamp above holds a row lock until this transaction ends, so a
	// conversion cannot land while the keys go down. A failure here rolls the
	// stamp back and leaves the trial armed for the next sweep, which a lockdown
	// after the commit would not get: a stamped row drops out of the sweep.
	//
	// DisableAPIKey writes its own disabled flag on the pool rather than on
	// dbtx, so a key already taken down stays down through that rollback. The
	// organization reads as enterprise with a dead key until the next sweep
	// completes the demotion.
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
		return fmt.Errorf("log trial demotion: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit trial demotion: %w", err)
	}

	d.notifyTrialInactive(ctx, args.OrganizationID)
	return nil
}

func (d *DemoteExpiredTrials) notifyTrialInactiveIfClosed(ctx context.Context, organizationID string) error {
	// GetActiveTrial is the armed-trial predicate. Re-read it immediately
	// before Loops so a re-arm that landed after MarkTrialDemoted's
	// ErrNoRows keeps trialActive. Lookup errors fail the activity so
	// Temporal retries; notifier errors stay logged-only.
	_, err := d.repo.GetActiveTrial(ctx, organizationID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		d.notifyTrialInactive(ctx, organizationID)
		return nil
	default:
		return fmt.Errorf("revalidate trial before inactive notify: %w", err)
	}
}

func (d *DemoteExpiredTrials) notifyTrialInactive(ctx context.Context, organizationID string) {
	if err := d.trial.TrialInactive(ctx, organizationID); err != nil {
		d.logger.ErrorContext(ctx, "failed to notify trial inactive", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
}
