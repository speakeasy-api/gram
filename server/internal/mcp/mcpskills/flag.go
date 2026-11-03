package mcpskills

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// FlagEnabled decides the skills-over-mcp rollout flag for an organization
// from the provider's locally cached definitions only. The decision sits on
// every initialize and server/discover of every surface that serves skills,
// so it must never wait on a remote flag service: the local path answers from
// the definitions the SDK polls in the background and fails closed (off) when
// they are unavailable or inconclusive, and a provider without local
// evaluation configured keeps the extension off everywhere. The flag is
// targeted by organization: the distinct id is the organization id and the
// PostHog group is its slug. An empty slug evaluates by organization id alone.
func FlagEnabled(ctx context.Context, logger *slog.Logger, features feature.Provider, organizationID, organizationSlug string) bool {
	if features == nil || organizationID == "" {
		return false
	}

	on, err := features.IsFlagEnabledLocal(ctx, feature.FlagSkillsOverMCP, organizationID, feature.OrgProjectGroups(organizationSlug, ""), nil)
	if err != nil {
		logger.WarnContext(ctx, "skills-over-mcp flag check failed; treating as disabled", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return false
	}
	return on
}
