package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
)

// BuildAgentPluginsView turns the marketplace-first rows from
// agent.GetAgentPluginSet into the tool-agnostic marketplaces + plugins view
// the agent endpoint returns.
//
// For every published marketplace in the org it emits the marketplace and its
// always-required observability plugin, then layers on the user's assigned
// plugins (the non-null plugin rows). Rows arrive grouped by project
// (`ORDER BY pr.id, p.slug`), one row per project even when the user has no
// assignment there (null plugin columns).
//
// The marketplace name and observability slug come from the shared `naming`
// package so they match exactly what the publish path wrote into the
// marketplace.json — tools resolve marketplaces by that name, so any mismatch
// silently fails to enable plugins.
//
// Note: gram publishes one marketplace name per *org* (naming.MarketplaceName
// is org-derived, not project-scoped), and a marketplace.json name is a single
// identifier. So if an org publishes multiple projects, they collapse to a
// single marketplace here — matching gram's existing publish limitation rather
// than introducing a new one. The collapse keeps the first row's token, and the
// query orders by pr.id so that first row is the org's default project (created
// at org setup, lowest id) rather than an arbitrary alphabetically-first one.
//
// marketplaceURL constructs the public marketplace git URL from a token; the
// caller owns the URL shape so this builder stays free of server-side config.
func BuildAgentPluginsView(rows []repo.GetAgentPluginSetRow, marketplaceURL func(token string) string) *gen.GetPluginsResult {
	var marketplaces []*gen.AgentMarketplace
	var plugins []*gen.AgentPlugin
	seenMarketplace := make(map[string]struct{})
	etag := sha256.New()

	for _, row := range rows {
		if !row.MarketplaceToken.Valid || row.MarketplaceToken.String == "" {
			continue
		}

		name := naming.MarketplaceName(row.OrganizationName)
		if _, ok := seenMarketplace[name]; !ok {
			seenMarketplace[name] = struct{}{}
			marketplaces = append(marketplaces, &gen.AgentMarketplace{
				Name: name,
				URL:  marketplaceURL(row.MarketplaceToken.String),
			})
			writeAgentPluginsETag(
				etag,
				"marketplace\x00%s\x00%s\x00%s\x00%d\n",
				name,
				row.OrganizationName,
				row.MarketplaceToken.String,
				row.MarketplaceUpdatedAt.Time.UnixNano(),
			)
			// Observability is required on every published marketplace,
			// independent of assignments.
			observabilitySlug := naming.ObservabilitySlug(row.OrganizationName)
			plugins = append(plugins, &gen.AgentPlugin{
				Slug:            observabilitySlug,
				MarketplaceName: name,
			})
			writeAgentPluginsETag(etag, "plugin\x00%s\x00%s\x00%d\n", name, observabilitySlug, int64(0))
		}

		// Assigned plugin for this project, if the LEFT JOIN matched one.
		if row.PluginID.Valid && row.PluginSlug.Valid {
			plugins = append(plugins, &gen.AgentPlugin{
				Slug:            row.PluginSlug.String,
				MarketplaceName: name,
			})
			writeAgentPluginsETag(etag, "plugin\x00%s\x00%s\x00%d\n", name, row.PluginSlug.String, row.PluginUpdatedAt.Time.UnixNano())
		}
	}

	return &gen.GetPluginsResult{
		Etag:         hex.EncodeToString(etag.Sum(nil)),
		Marketplaces: marketplaces,
		Plugins:      plugins,
	}
}

// writeAgentPluginsETag hashes only the contributions emitted into the result:
// the first marketplace for each rendered marketplace name, its observability
// plugin, and each rendered assigned plugin. Deployment config (e.g. the server
// URL) is deliberately excluded so it does not bust the cache.
func writeAgentPluginsETag(etag io.Writer, format string, args ...any) {
	// sha256.Hash never errors from Write; assign to _ to satisfy errcheck.
	_, _ = fmt.Fprintf(etag, format, args...)
}
