package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
)

// BuildAgentPluginsView regroups the flat rows returned by
// agent.GetAssignedPluginsForAgent into Claude Code's marketplaces+plugins
// shape: one AgentMarketplace per project (since marketplace_token is
// per-project) plus one AgentPlugin per assigned plugin pointing at its
// marketplace by name.
//
// Rows are assumed to arrive grouped by project_id (contiguous), which the
// query's `ORDER BY p.project_id, p.slug` guarantees.
//
// marketplaceURL constructs the public marketplace git URL from a token. The
// caller owns the URL shape so this builder stays free of server-side config.
func BuildAgentPluginsView(rows []repo.GetAssignedPluginsForAgentRow, marketplaceURL func(token string) string) *gen.GetPluginsResult {
	var marketplaces []*gen.AgentMarketplace
	var plugins []*gen.AgentPlugin
	seenMarketplace := make(map[string]struct{})

	for _, row := range rows {
		if !row.MarketplaceToken.Valid || row.MarketplaceToken.String == "" {
			// Defensive: the query already filters out null tokens, but keep
			// the guard so a future query change can't produce a half-formed
			// marketplace entry.
			continue
		}

		name := marketplaceName(row.OrganizationSlug, row.ProjectSlug)
		if _, ok := seenMarketplace[name]; !ok {
			seenMarketplace[name] = struct{}{}
			marketplaces = append(marketplaces, &gen.AgentMarketplace{
				Name:       name,
				URL:        marketplaceURL(row.MarketplaceToken.String),
				AutoUpdate: true,
			})
		}

		plugins = append(plugins, &gen.AgentPlugin{
			Slug:            row.PluginSlug,
			MarketplaceName: name,
		})
	}

	return &gen.GetPluginsResult{
		Etag:         computeAgentPluginsETag(rows),
		Marketplaces: marketplaces,
		Plugins:      plugins,
	}
}

// marketplaceName is the stable key the agent uses for this org+project's
// marketplace in Claude Code's extraKnownMarketplaces map. Picking a
// deterministic form so reconciliation across polls produces the same key.
func marketplaceName(orgSlug, projectSlug string) string {
	return "speakeasy-" + orgSlug + "-" + projectSlug
}

// computeAgentPluginsETag hashes the dimensions that determine the rendered
// marketplace + plugin set so a stable revision string can be returned to
// the agent. Excluded inputs (e.g. the server URL) are deployment-config,
// not policy — changing them should not bust the agent's cache.
func computeAgentPluginsETag(rows []repo.GetAssignedPluginsForAgentRow) string {
	h := sha256.New()
	for _, row := range rows {
		// sha256.Hash never errors from Write; assign to _ to satisfy errcheck.
		_, _ = fmt.Fprintf(
			h,
			"%s\x00%s\x00%d\x00%s\x00%d\n",
			row.PluginID.String(),
			row.PluginSlug,
			row.PluginUpdatedAt.Time.UnixNano(),
			row.MarketplaceToken.String,
			row.MarketplaceUpdatedAt.Time.UnixNano(),
		)
	}
	return hex.EncodeToString(h.Sum(nil))
}
