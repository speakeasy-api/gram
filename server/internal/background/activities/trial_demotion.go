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
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
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

type trialDemotionOpenRouter interface {
	AddAPIKeyDisableCauseWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, openrouter.DisableCause) (openrouter.DisableCauseChange, error)
	ReconcileAPIKeyDisabled(context.Context, string, openrouter.KeyType) error
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
		existing, getErr := tx.GetTrial(ctx, args.OrganizationID)
		if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
			return fmt.Errorf("read trial after no-op demotion: %w", getErr)
		}
		if errors.Is(getErr, pgx.ErrNoRows) || !existing.DemotedAt.Valid {
			d.logger.InfoContext(ctx, "expired trial changed before demotion", attr.SlogOrganizationID(args.OrganizationID))
			return nil
		}
		if _, lockErr := tx.LockTrialLifecycleForRearm(ctx, args.OrganizationID); lockErr != nil {
			return fmt.Errorf("lock no-op trial demotion lifecycle: %w", lockErr)
		}
		for _, keyType := range openrouter.AllKeyTypes {
			if lockErr := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, dbtx, args.OrganizationID, keyType); lockErr != nil {
				return fmt.Errorf("acquire no-op OpenRouter %s key billing lock: %w", keyType, lockErr)
			}
		}
		changedKeyTypes, selectErr := trialDemotionChangedKeyTypes(ctx, dbtx, args.OrganizationID)
		if selectErr != nil {
			return selectErr
		}
		if rollbackErr := dbtx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("close no-op trial demotion transaction: %w", rollbackErr)
		}
		return d.reconcileOpenRouterKeys(ctx, args.OrganizationID, changedKeyTypes)
	case err != nil:
		return fmt.Errorf("mark trial demoted: %w", err)
	}

	// MarkTrialDemoted locks the lifecycle row first. Take every key lock in the
	// canonical order before any key-row access, retaining them until commit.
	for _, keyType := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, dbtx, args.OrganizationID, keyType); err != nil {
			return fmt.Errorf("acquire OpenRouter %s key billing lock: %w", keyType, err)
		}
	}

	provisioner, ok := d.openRouter.(trialDemotionOpenRouter)
	if !ok {
		return errors.New("OpenRouter key provisioner cannot persist and reconcile trial demotion causes")
	}
	keyAccessChanged := false
	changedKeyTypes := make([]openrouter.KeyType, 0, len(openrouter.AllKeyTypes))
	for _, keyType := range openrouter.AllKeyTypes {
		change, err := provisioner.AddAPIKeyDisableCauseWithDB(ctx, dbtx, args.OrganizationID, keyType, openrouter.DisableCauseTrialDemotion)
		if err != nil {
			return fmt.Errorf("add trial demotion cause to OpenRouter %s key: %w", keyType, err)
		}
		keyAccessChanged = keyAccessChanged || change.KeyAccessChanged
		if change.KeyAccessChanged {
			changedKeyTypes = append(changedKeyTypes, keyType)
		}
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
		KeyAccessChanged:    keyAccessChanged,
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

	return d.reconcileOpenRouterKeys(ctx, args.OrganizationID, changedKeyTypes)
}

func trialDemotionChangedKeyTypes(ctx context.Context, db openrouter.DBTX, organizationID string) ([]openrouter.KeyType, error) {
	changedKeyTypes := make([]openrouter.KeyType, 0, len(openrouter.AllKeyTypes))
	for _, keyType := range openrouter.AllKeyTypes {
		key, err := openrouterrepo.New(db).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: organizationID,
			KeyType:        string(keyType),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return nil, fmt.Errorf("read OpenRouter %s key after no-op trial demotion: %w", keyType, err)
		case key.DisableCauses == nil:
			return nil, fmt.Errorf("OpenRouter %s key has unclassified causes after trial demotion", keyType)
		case len(key.DisableCauses) == 1 && key.DisableCauses[0] == string(openrouter.DisableCauseTrialDemotion):
			changedKeyTypes = append(changedKeyTypes, keyType)
		}
	}
	return changedKeyTypes, nil
}

func (d *DemoteExpiredTrials) reconcileOpenRouterKeys(ctx context.Context, organizationID string, keyTypes []openrouter.KeyType) error {
	provisioner, ok := d.openRouter.(trialDemotionOpenRouter)
	if !ok {
		return errors.New("OpenRouter key provisioner cannot reconcile trial demotion causes")
	}

	var reconcileErrors []error
	for _, keyType := range keyTypes {
		if err := provisioner.ReconcileAPIKeyDisabled(ctx, organizationID, keyType); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("reconcile OpenRouter %s key after trial demotion: %w", keyType, err))
		}
	}
	return errors.Join(reconcileErrors...)
}
