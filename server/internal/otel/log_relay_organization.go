package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const (
	// A short TTL limits rollout and recovery staleness while still removing
	// database and PostHog lookups from the per-batch hot path.
	logRelayOrganizationGateCacheTTL = 30 * time.Second

	// The subscriber sees logs from every organization, so cap the long tail of
	// inactive organizations without evicting the normal working set.
	logRelayOrganizationGateMaxSize = 4096
)

type logRelayOrganizationSlugResolver interface {
	OrganizationSlug(ctx context.Context, organizationID string) (string, error)
}

type logRelayOrganizationGate interface {
	Enabled(ctx context.Context, organizationID string) bool
}

type cachedLogRelayOrganizationGate struct {
	logger        *slog.Logger
	features      feature.Provider
	organizations logRelayOrganizationSlugResolver
	enabled       *expirable.LRU[string, bool]
	loads         singleflight.Group
}

func newLogRelayOrganizationGate(
	logger *slog.Logger,
	db database.DBTX,
	features feature.Provider,
) *cachedLogRelayOrganizationGate {
	return newCachedLogRelayOrganizationGate(
		logger,
		features,
		&postgresLogRelayOrganizationSlugResolver{db: db},
		logRelayOrganizationGateMaxSize,
		logRelayOrganizationGateCacheTTL,
	)
}

func newCachedLogRelayOrganizationGate(
	logger *slog.Logger,
	features feature.Provider,
	organizations logRelayOrganizationSlugResolver,
	maxSize int,
	ttl time.Duration,
) *cachedLogRelayOrganizationGate {
	return &cachedLogRelayOrganizationGate{
		logger:        logger,
		features:      features,
		organizations: organizations,
		enabled:       expirable.NewLRU[string, bool](maxSize, nil, ttl),
		loads:         singleflight.Group{},
	}
}

func (g *cachedLogRelayOrganizationGate) Enabled(ctx context.Context, organizationID string) bool {
	if organizationID == "" {
		return false
	}
	if enabled, ok := g.enabled.Get(organizationID); ok {
		return enabled
	}

	value, _, _ := g.loads.Do(organizationID, func() (any, error) {
		if enabled, ok := g.enabled.Get(organizationID); ok {
			return enabled, nil
		}

		// Cache unavailable evaluations as disabled. This both fails closed and
		// prevents an upstream outage from causing a lookup for every batch;
		// the short TTL bounds recovery delay.
		enabled := g.evaluate(ctx, organizationID)
		g.enabled.Add(organizationID, enabled)
		return enabled, nil
	})
	enabled, ok := value.(bool)
	if !ok {
		g.logger.WarnContext(
			ctx,
			"read cached customer log relay feature flag",
			attr.SlogOrganizationID(organizationID),
			attr.SlogError(fmt.Errorf("unexpected result type %T", value)),
		)
		return false
	}
	return enabled
}

func (g *cachedLogRelayOrganizationGate) evaluate(ctx context.Context, organizationID string) bool {
	organizationSlug, err := g.organizations.OrganizationSlug(ctx, organizationID)
	if err != nil {
		g.logger.WarnContext(
			ctx,
			"resolve organization for customer log relay feature flag",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false
	}
	if organizationSlug == "" {
		return false
	}

	enabled, err := g.features.IsFlagEnabled(
		ctx,
		feature.FlagOTELLogCustomerRelay,
		organizationID,
		feature.OrgProjectGroups(organizationSlug, ""),
	)
	if err != nil {
		g.logger.WarnContext(
			ctx,
			"evaluate customer log relay feature flag",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false
	}
	return enabled
}

type postgresLogRelayOrganizationSlugResolver struct {
	db database.DBTX
}

func (r *postgresLogRelayOrganizationSlugResolver) OrganizationSlug(
	ctx context.Context,
	organizationID string,
) (string, error) {
	if organizationID == "" {
		return "", nil
	}

	organization, err := organizationsrepo.New(r.db).GetOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get organization metadata: %w", err)
	}
	return organization.Slug, nil
}
