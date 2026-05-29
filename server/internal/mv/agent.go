package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// observabilitySlugSuffix is appended to the slugified org name to form the
// observability plugin's slug. It must match plugins.ClaudeObservabilitySlug
// (`conv.ToSlug(orgName) + "-observability"`) — the plugin is synthesized into
// every published marketplace at publish time, so the agent endpoint can only
// reference it by reconstructing the same slug here.
const observabilitySlugSuffix = "-observability"

// BuildAgentPluginsView turns the marketplace-first rows from
// agent.GetAgentPluginSet into Claude Code's marketplaces + plugins shape.
//
// For every published marketplace in the org it emits the marketplace and its
// always-required observability plugin, then layers on the user's assigned
// plugins (the non-null plugin rows). Rows arrive grouped by project
// (`ORDER BY pr.slug, p.slug`), one row per project even when the user has no
// assignment there (null plugin columns).
//
// marketplaceURL constructs the public marketplace git URL from a token; the
// caller owns the URL shape so this builder stays free of server-side config.
func BuildAgentPluginsView(rows []repo.GetAgentPluginSetRow, marketplaceURL func(token string) string) *gen.GetPluginsResult {
	var marketplaces []*gen.AgentMarketplace
	var plugins []*gen.AgentPlugin
	seenMarketplace := make(map[string]struct{})

	for _, row := range rows {
		if !row.MarketplaceToken.Valid || row.MarketplaceToken.String == "" {
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
			// Observability is required on every published marketplace,
			// independent of assignments.
			plugins = append(plugins, &gen.AgentPlugin{
				Slug:            conv.ToSlug(row.OrganizationName) + observabilitySlugSuffix,
				MarketplaceName: name,
			})
		}

		// Assigned plugin for this project, if the LEFT JOIN matched one.
		if row.PluginID.Valid && row.PluginSlug.Valid {
			plugins = append(plugins, &gen.AgentPlugin{
				Slug:            row.PluginSlug.String,
				MarketplaceName: name,
			})
		}
	}

	return &gen.GetPluginsResult{
		Etag:         computeAgentPluginsETag(rows),
		Marketplaces: marketplaces,
		Plugins:      plugins,
	}
}

func marketplaceName(orgSlug, projectSlug string) string {
	return "speakeasy-" + orgSlug + "-" + projectSlug
}

// computeAgentPluginsETag hashes the dimensions that determine the rendered
// set: the org name (drives the observability slug), marketplace tokens + their
// updated_at, and each assigned plugin's slug + updated_at. Deployment config
// (e.g. the server URL) is deliberately excluded so it doesn't bust the cache.
func computeAgentPluginsETag(rows []repo.GetAgentPluginSetRow) string {
	h := sha256.New()
	for _, row := range rows {
		// sha256.Hash never errors from Write; assign to _ to satisfy errcheck.
		_, _ = fmt.Fprintf(
			h,
			"%s\x00%s\x00%d\x00%s\x00%d\n",
			row.OrganizationName,
			row.MarketplaceToken.String,
			row.MarketplaceUpdatedAt.Time.UnixNano(),
			row.PluginSlug.String,
			row.PluginUpdatedAt.Time.UnixNano(),
		)
	}
	return hex.EncodeToString(h.Sum(nil))
}
