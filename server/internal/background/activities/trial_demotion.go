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
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type DemoteExpiredTrials struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *trialsrepo.Queries
	openRouter      openrouter.Provisioner
	audit           *audit.Logger
	notifier        trialemails.Notifier
	productFeatures *productfeatures.Client
}

func NewDemoteExpiredTrials(
	logger *slog.Logger,
	db *pgxpool.Pool,
	openRouterProvisioner openrouter.Provisioner,
	auditLogger *audit.Logger,
	notifier trialemails.Notifier,
	productFeatures *productfeatures.Client,
) *DemoteExpiredTrials {
	return &DemoteExpiredTrials{
		logger:          logger.With(attr.SlogComponent("demote_expired_trials")),
		db:              db,
		repo:            trialsrepo.New(db),
		openRouter:      openRouterProvisioner,
		audit:           auditLogger,
		notifier:        notifier,
		productFeatures: productFeatures,
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
		// A conversion or a manual reinstatement landed between the list and
		// this write. The organization is no longer ours to demote.
		d.logger.InfoContext(ctx, "expired trial changed before demotion",
			attr.SlogOrganizationID(args.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("mark trial demoted: %w", err)
	}

	// The stamp above holds a row lock until this transaction ends, so a
	// conversion cannot land while the keys go down. A failure here rolls the
	// stamp back and leaves the trial armed for the next sweep, which a lockdown
	// after the commit would not get: a stamped row drops out of the sweep.
	//
	// AddAPIKeyDisableCauseWithDB writes its cause on the locked key session,
	// outside dbtx, so a key already taken down stays down through an
	// organization-transaction rollback. The organization reads as enterprise
	// with a dead key until the next sweep completes the demotion.
	keyAccessChanged := false
	for _, keyType := range openrouter.AllKeyTypes {
		if err := keybillinglock.With(ctx, d.logger, d.db, args.OrganizationID, keyType, func(conn *pgxpool.Conn) error {
			dbProvisioner, ok := d.openRouter.(openRouterKeyBillingDBProvisioner)
			if !ok {
				return errors.New("OpenRouter key provisioner cannot use the locked database session")
			}
			change, err := dbProvisioner.AddAPIKeyDisableCauseWithDB(ctx, conn, args.OrganizationID, keyType, openrouter.DisableCauseTrialDemotion)
			if err != nil {
				return fmt.Errorf("add trial demotion disable cause: %w", err)
			}
			keyAccessChanged = keyAccessChanged || change.KeyAccessChanged
			return nil
		}); err != nil {
			return fmt.Errorf("disable openrouter %s key: %w", keyType, err)
		}
	}

	if keyAccessChanged {
		d.logger.DebugContext(ctx, "trial demotion changed platform key access", attr.SlogOrganizationID(args.OrganizationID))
	}

	if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, dbtx, args.OrganizationID, false); err != nil {
		return fmt.Errorf("disable trial runtime features: %w", err)
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
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		d.productFeatures.UpdateFeatureCache(ctx, args.OrganizationID, feature, false)
	}
	if d.notifier != nil {
		if err := d.notifier.TrialInactive(ctx, args.OrganizationID); err != nil {
			d.logger.ErrorContext(ctx, "notify inactive trial after demotion", attr.SlogOrganizationID(args.OrganizationID), attr.SlogError(err))
		}
	}

	return nil
}
