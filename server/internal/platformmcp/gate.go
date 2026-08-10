package platformmcp

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// CapabilityChecker is the durable organization capability boundary.
type CapabilityChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type OrganizationSlugResolver interface {
	OrganizationSlug(ctx context.Context, organizationID string) (string, error)
}

type PostgresOrganizationSlugResolver struct {
	db *pgxpool.Pool
}

func NewPostgresOrganizationSlugResolver(db *pgxpool.Pool) *PostgresOrganizationSlugResolver {
	return &PostgresOrganizationSlugResolver{db: db}
}

func (r *PostgresOrganizationSlugResolver) OrganizationSlug(ctx context.Context, organizationID string) (string, error) {
	if r == nil || r.db == nil || organizationID == "" {
		return "", ErrUnavailable
	}
	organization, err := organizationsrepo.New(r.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("get organization for Platform MCP rollout: %w", err)
	}
	return organization.Slug, nil
}

// OrganizationGate requires both the durable capability and transient rollout
// clearance. It intentionally fails closed when either provider is unavailable.
type OrganizationGate struct {
	capabilities  CapabilityChecker
	rollout       feature.Provider
	organizations OrganizationSlugResolver
}

func NewOrganizationGate(capabilities CapabilityChecker, rollout feature.Provider, organizations OrganizationSlugResolver) *OrganizationGate {
	return &OrganizationGate{capabilities: capabilities, rollout: rollout, organizations: organizations}
}

func (g *OrganizationGate) Enabled(ctx context.Context, organizationID string) (bool, error) {
	if g == nil || g.capabilities == nil || g.rollout == nil || g.organizations == nil || organizationID == "" {
		return false, ErrUnavailable
	}

	capable, err := g.capabilities.IsFeatureEnabled(ctx, organizationID, productfeatures.FeaturePlatformMCP)
	if err != nil {
		return false, fmt.Errorf("check platform mcp capability: %w", err)
	}
	if !capable {
		return false, nil
	}

	organizationSlug, err := resolveOrganizationSlug(ctx, g.organizations, organizationID)
	if err != nil {
		return false, err
	}

	rolledOut, err := g.rollout.IsFlagEnabled(ctx, feature.FlagPlatformMCPRollout, organizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return false, fmt.Errorf("check platform mcp rollout: %w", err)
	}
	return rolledOut, nil
}

// CatalogRegistrationGate adds the independent, default-off mutation rollout to
// the already-required Platform MCP organization gate. Read tools must not use
// this gate: catalog discovery and readiness remain available whenever the main
// Platform MCP gate permits them.
type CatalogRegistrationGate struct {
	platform      Gate
	rollout       feature.Provider
	organizations OrganizationSlugResolver
}

func NewCatalogRegistrationGate(platform Gate, rollout feature.Provider, organizations OrganizationSlugResolver) *CatalogRegistrationGate {
	return &CatalogRegistrationGate{
		platform:      platform,
		rollout:       rollout,
		organizations: organizations,
	}
}

func (g *CatalogRegistrationGate) Enabled(ctx context.Context, organizationID, projectSlug string) (bool, error) {
	if g == nil || g.platform == nil || g.rollout == nil || g.organizations == nil || organizationID == "" || projectSlug == "" {
		return false, ErrUnavailable
	}

	enabled, err := g.platform.Enabled(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("check platform mcp gate: %w", err)
	}
	if !enabled {
		return false, nil
	}

	organizationSlug, err := resolveOrganizationSlug(ctx, g.organizations, organizationID)
	if err != nil {
		return false, err
	}
	registrationEnabled, err := g.rollout.IsFlagEnabled(ctx, feature.FlagPlatformMCPCatalogRegistration, organizationID, feature.OrgProjectGroups(organizationSlug, projectSlug))
	if err != nil {
		return false, fmt.Errorf("check platform mcp catalog registration rollout: %w", err)
	}
	return registrationEnabled, nil
}

func resolveOrganizationSlug(ctx context.Context, organizations OrganizationSlugResolver, organizationID string) (string, error) {
	organizationSlug, err := organizations.OrganizationSlug(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("resolve platform mcp rollout organization: %w", err)
	}
	if organizationSlug == "" {
		return "", ErrUnavailable
	}
	return organizationSlug, nil
}
