package platformmcp

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// CapabilityChecker is the durable organization capability boundary.
type CapabilityChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// OrganizationGate requires both the durable capability and transient rollout
// clearance. It intentionally fails closed when either provider is unavailable.
type OrganizationGate struct {
	capabilities CapabilityChecker
	rollout      feature.Provider
}

func NewOrganizationGate(capabilities CapabilityChecker, rollout feature.Provider) *OrganizationGate {
	return &OrganizationGate{capabilities: capabilities, rollout: rollout}
}

func (g *OrganizationGate) Enabled(ctx context.Context, organizationID string) (bool, error) {
	if g == nil || g.capabilities == nil || g.rollout == nil || organizationID == "" {
		return false, ErrUnavailable
	}

	capable, err := g.capabilities.IsFeatureEnabled(ctx, organizationID, productfeatures.FeaturePlatformMCP)
	if err != nil {
		return false, fmt.Errorf("check platform mcp capability: %w", err)
	}
	if !capable {
		return false, nil
	}

	rolledOut, err := g.rollout.IsFlagEnabled(ctx, feature.FlagPlatformMCPRollout, organizationID, nil)
	if err != nil {
		return false, fmt.Errorf("check platform mcp rollout: %w", err)
	}
	return rolledOut, nil
}
