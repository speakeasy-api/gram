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

// OrganizationGate combines the engineering-owned Platform MCP rollout with the
// organization-admin entitlement. Both must be enabled: the PostHog flag keeps
// an unreleased surface inaccessible, while the durable product feature lets an
// organization opt out after release. Any unavailable dependency fails closed.
type OrganizationGate struct {
	capabilities  CapabilityChecker
	flags         feature.Provider
	organizations OrganizationSlugResolver
}

func NewOrganizationGate(capabilities CapabilityChecker, flags feature.Provider, organizations OrganizationSlugResolver) *OrganizationGate {
	return &OrganizationGate{
		capabilities:  capabilities,
		flags:         flags,
		organizations: organizations,
	}
}

func (g *OrganizationGate) Enabled(ctx context.Context, organizationID string) (bool, error) {
	if g == nil || g.capabilities == nil || g.flags == nil || g.organizations == nil || organizationID == "" {
		return false, ErrUnavailable
	}

	organizationSlug, err := g.organizations.OrganizationSlug(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("resolve organization for platform mcp rollout: %w", err)
	}
	rollout, err := g.flags.IsFlagEnabled(ctx, feature.FlagPlatformMCP, organizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return false, fmt.Errorf("check platform mcp rollout: %w", err)
	}
	if !rollout {
		return false, nil
	}

	capable, err := g.capabilities.IsFeatureEnabled(ctx, organizationID, productfeatures.FeaturePlatformMCP)
	if err != nil {
		return false, fmt.Errorf("check platform mcp capability: %w", err)
	}
	return capable, nil
}

// CatalogRegistrationGate ensures mutations require the main Platform MCP
// capability and an explicit project slug. Dashboard visibility is not checked:
// an enabled organization may use the MCP through manual setup alone.
type CatalogRegistrationGate struct {
	platform Gate
}

func NewCatalogRegistrationGate(platform Gate) *CatalogRegistrationGate {
	return &CatalogRegistrationGate{platform: platform}
}

func (g *CatalogRegistrationGate) Enabled(ctx context.Context, organizationID, projectSlug string) (bool, error) {
	if g == nil || g.platform == nil || organizationID == "" || projectSlug == "" {
		return false, ErrUnavailable
	}

	enabled, err := g.platform.Enabled(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("check platform mcp gate: %w", err)
	}
	return enabled, nil
}
