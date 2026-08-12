package platformmcp

import (
	"context"
	"fmt"
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
	gate        Gate
	eligibility NewModelEligibility
}

func NewAdmissionChecker(gate Gate, eligibility NewModelEligibility) *AdmissionChecker {
	return &AdmissionChecker{
		gate:        gate,
		eligibility: eligibility,
	}
}

func (c *AdmissionChecker) Evaluate(ctx context.Context, organizationID, _ string) (Admission, error) {
	if c == nil || c.gate == nil || c.eligibility == nil || organizationID == "" {
		return AdmissionIndeterminate, nil
	}
	if err := ctx.Err(); err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp admission context: %w", err)
	}

	enabled, err := c.gate.Enabled(ctx, organizationID)
	if err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp gate: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return AdmissionIndeterminate, fmt.Errorf("check platform mcp admission context after gate: %w", err)
	}
	if !enabled {
		return AdmissionDisabled, nil
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
