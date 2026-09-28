package productfeatures

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type MutationActor struct {
	Principal   urn.Principal
	DisplayName *string
	Slug        *string
}

type Mutator struct {
	client *Client
	audit  *audit.Logger
}

func NewMutator(client *Client, auditLogger *audit.Logger) *Mutator {
	return &Mutator{client: client, audit: auditLogger}
}

const mutationCacheTimeout = 5 * time.Second

func mutationCacheContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), mutationCacheTimeout)
}

func rollbackTransaction(ctx context.Context, rollback func(context.Context) error) error {
	return rollback(context.WithoutCancel(ctx))
}

// MutationLockFeatures lists the feature cache locks a change to feature must
// hold. Remote session auto-refresh also clears its enforcement flag.
func MutationLockFeatures(feature Feature) []Feature {
	if feature == FeatureRemoteSessionAutoRefresh {
		return []Feature{FeatureRemoteSessionAutoRefresh, FeatureRemoteSessionAutoRefreshEnforced}
	}
	return []Feature{feature}
}

// LockFeatureChange acquires the locks for a change to feature. The caller must
// begin its transaction on the returned connection, call ApplyFeatureChangeTx,
// commit, call StoreCommittedFeatureChange, and only then release.
func (m *Mutator) LockFeatureChange(ctx context.Context, organizationID string, feature Feature) (*pgxpool.Conn, func(), error) {
	conn, release, err := m.client.acquireFeatureCacheLocks(ctx, organizationID, MutationLockFeatures(feature))
	if err != nil {
		return nil, nil, fmt.Errorf("lock feature cache state: %w", err)
	}
	return conn, release, nil
}

// ApplyFeatureChangeTx makes one feature change and its audit record inside
// dbtx, dispatching remote session auto-refresh to its policy-preserving path.
// It reports whether durable state changed. The caller must hold the locks
// from LockFeatureChange on the connection that owns dbtx.
func (m *Mutator) ApplyFeatureChangeTx(ctx context.Context, dbtx pgx.Tx, organizationID string, feature Feature, enabled bool, actor MutationActor) (bool, error) {
	if feature == FeatureRemoteSessionAutoRefresh {
		return m.applyRemoteSessionAutoRefreshTx(ctx, dbtx, organizationID, enabled, actor)
	}
	return m.applyFeatureTx(ctx, dbtx, organizationID, feature, enabled, actor)
}

// StoreCommittedFeatureChange refreshes the cache after a committed change.
// Call it while the locks from LockFeatureChange are still held.
func (m *Mutator) StoreCommittedFeatureChange(ctx context.Context, organizationID string, feature Feature, enabled bool) {
	cacheCtx, cancel := mutationCacheContext(ctx)
	defer cancel()
	if feature == FeatureRemoteSessionAutoRefresh {
		_ = m.client.storeFeatureCache(cacheCtx, organizationID, FeatureRemoteSessionAutoRefreshEnforced, false, "failed to cache remote session refresh policy")
		_ = m.client.storeFeatureCache(cacheCtx, organizationID, FeatureRemoteSessionAutoRefresh, enabled, "failed to cache remote session refresh policy")
		return
	}
	if feature == FeatureSkills && !enabled {
		return
	}
	_ = m.client.storeFeatureCache(cacheCtx, organizationID, feature, enabled, "failed to cache feature flag state")
}

func (m *Mutator) SetFeature(ctx context.Context, organizationID string, feature Feature, enabled bool, actor MutationActor) error {
	if feature == FeatureRemoteSessionAutoRefreshEnforced {
		return oops.E(oops.CodeInvalid, nil, "remote session auto-refresh enforcement must be changed through the policy setter")
	}

	// Skills is always on, so disabling it remains a silent no-op.
	if feature == FeatureSkills && !enabled {
		return nil
	}

	lockConn, releaseFeatureLock, err := m.client.acquireFeatureCacheLocks(ctx, organizationID, []Feature{feature})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock feature cache state").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	defer releaseFeatureLock()

	dbtx, err := lockConn.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin feature flag transaction").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	defer o11y.NoLogDefer(func() error { return rollbackTransaction(ctx, dbtx.Rollback) })

	if _, err := m.applyFeatureTx(ctx, dbtx, organizationID, feature, enabled, actor); err != nil {
		return err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit feature flag change").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}

	m.StoreCommittedFeatureChange(ctx, organizationID, feature, enabled)
	return nil
}

func (m *Mutator) applyFeatureTx(ctx context.Context, dbtx pgx.Tx, organizationID string, feature Feature, enabled bool, actor MutationActor) (bool, error) {
	if feature == FeatureRemoteSessionAutoRefreshEnforced {
		return false, oops.E(oops.CodeInvalid, nil, "remote session auto-refresh enforcement must be changed through the policy setter")
	}
	if feature == FeatureSkills && !enabled {
		return false, nil
	}

	// Derive changed from the write itself so audit records exactly the
	// transition that commits, without a read-then-write race.
	q := repo.New(dbtx)
	changed := false
	if enabled && feature == FeatureSkills {
		inserted, err := EnableSkillsTx(ctx, dbtx, organizationID)
		if err != nil {
			return false, oops.E(oops.CodeUnexpected, err, "enable Skills feature").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		}
		changed = inserted
	} else if enabled {
		inserted, err := q.EnableFeature(ctx, repo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
		if err != nil {
			return false, oops.E(oops.CodeUnexpected, err, "enable organization feature flag %q", feature).LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		}
		changed = inserted > 0
	} else {
		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return false, oops.E(oops.CodeUnexpected, err, "disable organization feature flag %q", feature).LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		default:
			changed = true
		}
	}

	if changed {
		if err := m.logFeatureToggled(ctx, dbtx, organizationID, feature, enabled, actor); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func (m *Mutator) logFeatureToggled(ctx context.Context, dbtx pgx.Tx, organizationID string, feature Feature, enabled bool, actor MutationActor) error {
	org, err := orgrepo.New(dbtx).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read organization for feature toggle audit event").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	if err := m.audit.LogOrganizationProductFeatureToggled(ctx, dbtx, audit.LogOrganizationProductFeatureToggledEvent{
		OrganizationID: organizationID, Actor: actor.Principal, ActorDisplayName: actor.DisplayName, ActorSlug: actor.Slug,
		OrganizationName: org.Name, OrganizationSlug: org.Slug, FeatureName: string(feature), FeatureEnabled: enabled,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record feature toggle audit event").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	return nil
}

func (m *Mutator) SetRemoteSessionAutoRefreshEnabled(ctx context.Context, organizationID string, enabled bool, actor MutationActor) error {
	lockConn, releaseFeatureLocks, err := m.client.acquireFeatureCacheLocks(ctx, organizationID, MutationLockFeatures(FeatureRemoteSessionAutoRefresh))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock remote session refresh cache state").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	defer releaseFeatureLocks()

	dbtx, err := lockConn.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin remote session refresh transaction").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	defer o11y.NoLogDefer(func() error { return rollbackTransaction(ctx, dbtx.Rollback) })

	if _, err := m.applyRemoteSessionAutoRefreshTx(ctx, dbtx, organizationID, enabled, actor); err != nil {
		return err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit remote session refresh change").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}

	m.StoreCommittedFeatureChange(ctx, organizationID, FeatureRemoteSessionAutoRefresh, enabled)
	return nil
}

func (m *Mutator) applyRemoteSessionAutoRefreshTx(ctx context.Context, dbtx pgx.Tx, organizationID string, enabled bool, actor MutationActor) (bool, error) {
	q := repo.New(dbtx)
	setFeatureState := func(feature Feature, state bool) (bool, error) {
		if state {
			inserted, err := q.EnableFeature(ctx, repo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
			if err != nil {
				return false, fmt.Errorf("enable feature %q: %w", feature, err)
			}
			return inserted > 0, nil
		}
		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("disable feature %q: %w", feature, err)
		}
		return true, nil
	}

	enforcedChanged, err := setFeatureState(FeatureRemoteSessionAutoRefreshEnforced, false)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "clear remote session refresh enforcement").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	visibleChanged, err := setFeatureState(FeatureRemoteSessionAutoRefresh, enabled)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "set remote session refresh visibility").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}

	changed := enforcedChanged || visibleChanged
	if changed {
		if err := m.logFeatureToggled(ctx, dbtx, organizationID, FeatureRemoteSessionAutoRefresh, enabled, actor); err != nil {
			return false, err
		}
	}
	return changed, nil
}
