package productfeatures

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

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

func rollbackTransaction(ctx context.Context, rollback func(context.Context) error) error {
	return rollback(context.WithoutCancel(ctx))
}

func (m *Mutator) SetFeature(ctx context.Context, organizationID string, feature Feature, enabled bool, actor MutationActor) error {
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

	// Derive changed from the write itself so audit records exactly the
	// transition that commits, without a read-then-write race.
	q := repo.New(dbtx)
	changed := false
	if enabled && feature == FeatureSkills {
		inserted, err := EnableSkillsTx(ctx, dbtx, organizationID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable Skills feature").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		}
		changed = inserted
	} else if enabled {
		inserted, err := q.EnableFeature(ctx, repo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable organization feature flag %q", feature).LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		}
		changed = inserted > 0
	} else {
		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "disable organization feature flag %q", feature).LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		default:
			changed = true
		}
	}

	if changed {
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
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit feature flag change").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}

	_ = m.client.storeFeatureCache(ctx, organizationID, feature, enabled, "failed to cache feature flag state")
	return nil
}

func (m *Mutator) SetRemoteSessionAutoRefreshEnabled(ctx context.Context, organizationID string, enabled bool, actor MutationActor) error {
	lockConn, releaseFeatureLocks, err := m.client.acquireFeatureCacheLocks(ctx, organizationID, []Feature{
		FeatureRemoteSessionAutoRefresh, FeatureRemoteSessionAutoRefreshEnforced,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock remote session refresh cache state").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	defer releaseFeatureLocks()

	dbtx, err := lockConn.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin remote session refresh transaction").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	defer o11y.NoLogDefer(func() error { return rollbackTransaction(ctx, dbtx.Rollback) })

	q := repo.New(dbtx)
	setFeatureState := func(feature Feature, state bool) (bool, error) {
		if state {
			inserted, err := q.EnableFeature(ctx, repo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
			return inserted > 0, err
		}
		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}

	enforcedChanged, err := setFeatureState(FeatureRemoteSessionAutoRefreshEnforced, false)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear remote session refresh enforcement").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}
	visibleChanged, err := setFeatureState(FeatureRemoteSessionAutoRefresh, enabled)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "set remote session refresh visibility").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}

	if enforcedChanged || visibleChanged {
		org, err := orgrepo.New(dbtx).GetOrganizationMetadata(ctx, organizationID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "read organization for feature toggle audit event").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		}
		if err := m.audit.LogOrganizationProductFeatureToggled(ctx, dbtx, audit.LogOrganizationProductFeatureToggledEvent{
			OrganizationID: organizationID, Actor: actor.Principal, ActorDisplayName: actor.DisplayName, ActorSlug: actor.Slug,
			OrganizationName: org.Name, OrganizationSlug: org.Slug, FeatureName: string(FeatureRemoteSessionAutoRefresh), FeatureEnabled: enabled,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record feature toggle audit event").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit remote session refresh change").LogError(ctx, m.client.logger, attr.SlogOrganizationID(organizationID))
	}

	_ = m.client.storeFeatureCache(ctx, organizationID, FeatureRemoteSessionAutoRefreshEnforced, false, "failed to cache remote session refresh policy")
	_ = m.client.storeFeatureCache(ctx, organizationID, FeatureRemoteSessionAutoRefresh, enabled, "failed to cache remote session refresh policy")
	return nil
}
