package platformmcp

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// CapabilityChecker is the durable organization capability boundary. Platform
// MCP checks bypass the shared 15-minute product-feature cache so revocation is
// visible on the next request, comfortably within the 60-second requirement.
type CapabilityChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
	IsFeatureEnabledUncached(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
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
	if g == nil {
		return false, ErrUnavailable
	}
	return evaluateTwoKeyGate(ctx, g.capabilities, g.flags, g.organizations, organizationID, twoKeyGateSurface{
		flag:              feature.FlagPlatformMCP,
		resolveContext:    "resolve organization for platform mcp rollout",
		rolloutContext:    "check platform mcp rollout",
		capabilityContext: "check platform mcp capability",
	})
}

// twoKeyGateSurface carries what distinguishes one two-key gate from another:
// the surface's PostHog rollout flag and the error contexts its wrapped
// failures name.
type twoKeyGateSurface struct {
	flag              feature.Flag
	resolveContext    string
	rolloutContext    string
	capabilityContext string
}

// evaluateTwoKeyGate is the shared two-key rollout evaluation: the
// surface-specific PostHog flag keeps an unreleased surface inaccessible,
// while the durable Platform MCP product feature — read uncached so
// revocation is visible on the next request — lets an organization opt out
// after release. Both keys must be enabled, and any unavailable dependency
// fails closed.
func evaluateTwoKeyGate(ctx context.Context, capabilities CapabilityChecker, flags feature.Provider, organizations OrganizationSlugResolver, organizationID string, surface twoKeyGateSurface) (bool, error) {
	if capabilities == nil || flags == nil || organizations == nil || organizationID == "" {
		return false, ErrUnavailable
	}

	organizationSlug, err := organizations.OrganizationSlug(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("%s: %w", surface.resolveContext, err)
	}
	if organizationSlug == "" {
		return false, ErrUnavailable
	}
	rollout, err := flags.IsFlagEnabled(ctx, surface.flag, organizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return false, fmt.Errorf("%s: %w", surface.rolloutContext, err)
	}
	if !rollout {
		return false, nil
	}

	capable, err := capabilities.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeaturePlatformMCP)
	if err != nil {
		return false, fmt.Errorf("%s: %w", surface.capabilityContext, err)
	}
	return capable, nil
}

// RemoteMCPSurfaceGate admits the remote URL source registration tool pair —
// probe_remote_mcp and register_remote_mcp_for_project — following the
// OrganizationGate two-key pattern: the surface-specific PostHog flag keeps an
// unreleased surface inaccessible, while the durable Platform MCP product
// feature lets an organization opt out after release. Both must be enabled,
// and any unavailable dependency fails closed. The main Platform MCP rollout
// is enforced separately at request admission, so this gate only adds the
// keys specific to the user-supplied-URL surface.
type RemoteMCPSurfaceGate struct {
	capabilities  CapabilityChecker
	flags         feature.Provider
	organizations OrganizationSlugResolver
}

func NewRemoteMCPSurfaceGate(capabilities CapabilityChecker, flags feature.Provider, organizations OrganizationSlugResolver) *RemoteMCPSurfaceGate {
	return &RemoteMCPSurfaceGate{
		capabilities:  capabilities,
		flags:         flags,
		organizations: organizations,
	}
}

func (g *RemoteMCPSurfaceGate) Enabled(ctx context.Context, organizationID string) (bool, error) {
	if g == nil {
		return false, ErrUnavailable
	}
	return evaluateTwoKeyGate(ctx, g.capabilities, g.flags, g.organizations, organizationID, twoKeyGateSurface{
		flag:              feature.FlagPlatformMCPRemoteURL,
		resolveContext:    "resolve organization for remote mcp registration rollout",
		rolloutContext:    "check remote mcp registration rollout",
		capabilityContext: "check platform mcp capability for remote mcp registration",
	})
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
