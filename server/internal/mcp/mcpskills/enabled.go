package mcpskills

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// FeatureChecker reports whether a product feature is enabled for an
// organization. It matches productfeatures.Client.PlatformFeatureCheck and
// fails closed: lookup errors read as disabled.
type FeatureChecker func(ctx context.Context, organizationID string, feature string) bool

// Enabled decides whether an organization is served skills over MCP: the
// staff-managed skills_over_mcp product feature, toggled per organization
// from the admin dashboard. It is off for every organization by default. The
// decision sits on every initialize and server/discover of every surface that
// serves skills and is read through the shared feature cache. A nil checker or
// an empty organization reads as off.
func Enabled(ctx context.Context, entitled FeatureChecker, organizationID string) bool {
	if entitled == nil || organizationID == "" {
		return false
	}
	return entitled(ctx, organizationID, string(productfeatures.FeatureSkillsOverMCP))
}
