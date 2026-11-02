package mcpskills

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// FeatureChecker reports whether a product feature is enabled for an
// organization. It matches productfeatures.Client.PlatformFeatureCheck and
// fails closed: lookup errors read as disabled.
type FeatureChecker func(ctx context.Context, organizationID string, feature string) bool

// FlagEnabled decides whether an organization is in the skills-over-mcp
// rollout. It is on when either the PostHog flag or the staff-managed
// skills_over_mcp product feature (toggled per organization from the admin
// dashboard) is on.
//
// The PostHog flag is decided from the provider's locally cached definitions
// only. The decision sits on every initialize and server/discover of every
// surface that serves skills, so it must never wait on a remote flag service:
// the local path answers from the definitions the SDK polls in the background
// and fails closed (off) when they are unavailable or inconclusive. The flag
// is targeted by organization: the distinct id is the organization id and the
// PostHog group is its slug. An empty slug evaluates by organization id alone.
// The product feature is read through the shared feature cache and is only
// consulted when the flag is off. A nil provider or checker skips that source.
func FlagEnabled(ctx context.Context, logger *slog.Logger, features feature.Provider, entitled FeatureChecker, organizationID, organizationSlug string) bool {
	if organizationID == "" {
		return false
	}

	if features != nil {
		on, err := features.IsFlagEnabledLocal(ctx, feature.FlagSkillsOverMCP, organizationID, feature.OrgProjectGroups(organizationSlug, ""), nil)
		switch {
		case err != nil:
			logger.WarnContext(ctx, "skills-over-mcp flag check failed; treating as disabled", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		case on:
			return true
		}
	}

	if entitled != nil {
		return entitled(ctx, organizationID, string(productfeatures.FeatureSkillsOverMCP))
	}
	return false
}
