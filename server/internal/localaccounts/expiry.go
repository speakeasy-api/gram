package localaccounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/localaccounts/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// HandleTrialFixture excludes explicitly marked fixtures from the real trial
// lifecycle. Install this handler only in the local environment. Expiry changes
// local access without altering keys, resources, timestamps or trial deadlines.
func HandleTrialFixture(ctx context.Context, db *pgxpool.Pool, features *productfeatures.Client, orgID string) (bool, error) {
	exists, err := repo.New(db).AccountProfilesExist(ctx)
	if err != nil {
		return false, fmt.Errorf("check local trial schema: %w", err)
	}
	if !exists {
		return false, nil
	}
	profile, err := repo.New(db).GetAccountProfileName(ctx, conv.ToPGText(orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read local trial marker: %w", err)
	}
	if Profile(profile) != ActiveTrial {
		return true, nil
	}
	if features == nil {
		return true, errors.New("local trial expiry requires product features")
	}
	conn, release, err := features.AcquireFeatureCacheLocks(ctx, orgID, profileFeatures())
	if err != nil {
		return true, fmt.Errorf("acquire trial feature locks: %w", err)
	}
	defer release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return true, fmt.Errorf("begin local trial expiry: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	if err = repo.New(tx).LockAccountProfile(ctx); err != nil {
		return true, fmt.Errorf("lock local trial profile: %w", err)
	}
	// Serialize with profile application, then re-read marker and actual trial.
	profile, err = repo.New(tx).GetAccountProfileName(ctx, conv.ToPGText(orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("recheck local trial marker: %w", err)
	}
	if Profile(profile) != ActiveTrial {
		return true, nil
	}
	// Match profile application and trial-start ordering before checking expiry.
	if err = repo.New(tx).LockAccountMetadata(ctx, orgID); err != nil {
		return true, fmt.Errorf("lock expiry account metadata: %w", err)
	}
	if err = repo.New(tx).LockAccountTrial(ctx, orgID); err != nil {
		return true, fmt.Errorf("lock expiring account trial: %w", err)
	}
	due, err := repo.New(tx).AccountTrialDue(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, fmt.Errorf("check local trial deadline: %w", err)
	}
	if due.Bool {
		if err = repo.New(tx).ExpireAccountTier(ctx, orgID); err != nil {
			return true, fmt.Errorf("expire local account tier: %w", err)
		}
		if err = repo.New(tx).DemoteAccountTrial(ctx, orgID); err != nil {
			return true, fmt.Errorf("demote local trial: %w", err)
		}
		if err = productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, orgID, false); err != nil {
			return true, fmt.Errorf("disable local trial runtime: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("commit local trial expiry: %w", err)
	}
	// Retry cache refresh even after a previous projection committed successfully.
	var refreshErrors []error
	for _, feature := range profileFeatures() {
		if err = features.UpdateFeatureCacheUnderLock(ctx, conn, orgID, feature); err != nil {
			refreshErrors = append(refreshErrors, err)
		}
	}
	return true, errors.Join(refreshErrors...)
}
