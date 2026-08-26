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

	// Keep both shared cache fills and individual callers bounded independently.
	logRelayOrganizationGateLookupTimeout = time.Second
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
	lookupTimeout time.Duration
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
		logRelayOrganizationGateLookupTimeout,
	)
}

func newCachedLogRelayOrganizationGate(
	logger *slog.Logger,
	features feature.Provider,
	organizations logRelayOrganizationSlugResolver,
	maxSize int,
	ttl time.Duration,
	lookupTimeout time.Duration,
) *cachedLogRelayOrganizationGate {
	return &cachedLogRelayOrganizationGate{
		logger:        logger,
		features:      features,
		organizations: organizations,
		enabled:       expirable.NewLRU[string, bool](maxSize, nil, ttl),
		loads:         singleflight.Group{},
		lookupTimeout: lookupTimeout,
	}
}

func (g *cachedLogRelayOrganizationGate) Enabled(ctx context.Context, organizationID string) bool {
	if organizationID == "" {
		return false
	}
	if enabled, ok := g.enabled.Get(organizationID); ok {
		return enabled
	}

	results := g.loads.DoChan(organizationID, func() (any, error) {
		if enabled, ok := g.enabled.Get(organizationID); ok {
			return enabled, nil
		}

		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), g.lookupTimeout)
		defer cancel()

		enabled, err := g.evaluate(lookupCtx, organizationID)
		if err != nil {
			return false, err
		}
		g.enabled.Add(organizationID, enabled)
		return enabled, nil
	})

	waitCtx, cancel := context.WithTimeout(ctx, g.lookupTimeout)
	defer cancel()

	var result singleflight.Result
	select {
	case result = <-results:
	case <-waitCtx.Done():
		return false
	}
	if result.Err != nil {
		return false
	}

	enabled, ok := result.Val.(bool)
	if !ok {
		g.logger.WarnContext(
			ctx,
			"resolve customer log relay feature flag",
			attr.SlogOrganizationID(organizationID),
			attr.SlogError(fmt.Errorf("unexpected result type %T", result.Val)),
		)
		return false
	}
	return enabled
}

func (g *cachedLogRelayOrganizationGate) evaluate(ctx context.Context, organizationID string) (bool, error) {
	organizationSlug, err := g.organizations.OrganizationSlug(ctx, organizationID)
	if err != nil {
		g.logger.WarnContext(
			ctx,
			"resolve organization for customer log relay feature flag",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false, fmt.Errorf("resolve organization slug: %w", err)
	}
	if organizationSlug == "" {
		return false, nil
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
		return false, fmt.Errorf("evaluate feature flag: %w", err)
	}
	return enabled, nil
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
