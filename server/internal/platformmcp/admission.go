package platformmcp

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type Admission uint8

const (
	AdmissionIndeterminate Admission = iota
	AdmissionDisabled
	AdmissionEnabled
)

type NewModelEligibility interface {
	EligibleForPlatformMCP(ctx context.Context, organizationID string) (bool, error)
}

// AdmissionChecker resolves whether the first-party Platform MCP package may be
// introduced or removed. It is intentionally separate from Runtime Gate: an
// indeterminate package decision must preserve published bytes, while runtime
// requests still fail closed.
type AdmissionChecker struct {
	capabilities CapabilityChecker
	rollout      feature.Provider
	eligibility  NewModelEligibility
}

func NewAdmissionChecker(capabilities CapabilityChecker, rollout feature.Provider, eligibility NewModelEligibility) *AdmissionChecker {
	return &AdmissionChecker{
		capabilities: capabilities,
		rollout:      rollout,
		eligibility:  eligibility,
	}
}

func (c *AdmissionChecker) Evaluate(ctx context.Context, organizationID, organizationSlug string) (Admission, error) {
	if c == nil || c.capabilities == nil || c.rollout == nil || c.eligibility == nil || organizationID == "" || organizationSlug == "" {
		return AdmissionIndeterminate, nil
	}
	if err := ctx.Err(); err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp admission context: %w", err)
	}

	capable, err := c.capabilities.IsFeatureEnabled(ctx, organizationID, productfeatures.FeaturePlatformMCP)
	if err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp capability: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp admission context after capability: %w", err)
	}
	if !capable {
		return AdmissionDisabled, nil
	}

	rollout, err := feature.EvaluateFlag(ctx, c.rollout, feature.FlagPlatformMCPRollout, organizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return AdmissionIndeterminate, fmt.Errorf("evaluate platform mcp rollout: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp admission context after rollout: %w", err)
	}
	switch rollout {
	case feature.EvaluationEnabled:
	case feature.EvaluationDisabled:
		return AdmissionDisabled, nil
	case feature.EvaluationIndeterminate:
		return AdmissionIndeterminate, nil
	default:
		return AdmissionIndeterminate, nil
	}

	eligible, err := c.eligibility.EligibleForPlatformMCP(ctx, organizationID)
	if err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp new-model eligibility: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp admission context after eligibility: %w", err)
	}
	if !eligible {
		return AdmissionDisabled, nil
	}

	return AdmissionEnabled, nil
}
