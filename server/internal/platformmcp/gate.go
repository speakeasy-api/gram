package platformmcp

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// CapabilityChecker is the durable organization capability boundary. Platform
// MCP checks bypass the shared 15-minute product-feature cache so revocation is
// visible on the next request, comfortably within the 60-second requirement.
type CapabilityChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
	IsFeatureEnabledUncached(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// OrganizationGate enforces the durable organization-admin entitlement. Any
// unavailable dependency fails closed.
type OrganizationGate struct {
	capabilities CapabilityChecker
}

func NewOrganizationGate(capabilities CapabilityChecker) *OrganizationGate {
	return &OrganizationGate{capabilities: capabilities}
}

func (g *OrganizationGate) Enabled(ctx context.Context, organizationID string) (bool, error) {
	if g == nil || g.capabilities == nil || organizationID == "" {
		return false, ErrUnavailable
	}

	capable, err := g.capabilities.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeaturePlatformMCP)
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
	if projectSlug == "" {
		return false, ErrUnavailable
	}
	return g.EnabledOrganization(ctx, organizationID)
}

// EnabledOrganization checks the same durable entitlement as a mutation
// without accepting a project selector. Read-only direct inspection
// needs this gate before it can perform user-directed egress.
func (g *CatalogRegistrationGate) EnabledOrganization(ctx context.Context, organizationID string) (bool, error) {
	if g == nil || g.platform == nil || organizationID == "" {
		return false, ErrUnavailable
	}
	enabled, err := g.platform.Enabled(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("check platform mcp gate: %w", err)
	}
	return enabled, nil
}
