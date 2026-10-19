package feature

import (
	"context"
	"fmt"
)

// HostedMCPServerKeyWrites reports whether hosted MCP dependents are written
// under the canonical MCP server key. Anything but an explicit enable keeps the
// toolset key, so a missing provider, organization, or flag is phase A.
func HostedMCPServerKeyWrites(ctx context.Context, provider Provider, organizationID, organizationSlug string) (bool, error) {
	if organizationID == "" {
		return false, nil
	}
	evaluation, err := EvaluateFlag(ctx, provider, FlagHostedMCPServerKeyWrites, organizationID, OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return false, fmt.Errorf("evaluate hosted mcp server key writes flag: %w", err)
	}
	return evaluation == EvaluationEnabled, nil
}
