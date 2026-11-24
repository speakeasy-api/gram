package feature

import "context"

// AgentMCPProjectBoundaryEnforced reports whether the organization enforces
// the project boundary on its agent MCP gateways. Any evaluation failure keeps
// enforcement off while shadow observations continue.
func AgentMCPProjectBoundaryEnforced(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagAgentMCPProjectBoundaryEnforcement, organizationID, nil, nil)
	return err == nil && enabled
}
