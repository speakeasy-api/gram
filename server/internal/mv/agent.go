package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/google/uuid"

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
// Each project's marketplace name is resolved the same way the publish path
// resolves it: the per-project override (project_marketplace_settings) when set,
// else the shared `naming` default — the bare org-derived name for the org's
// default project, project-scoped (`<org>-<project>-speakeasy`) for the rest.
// Matching the publish path exactly matters because tools resolve marketplaces
// by that name — any mismatch silently fails to enable plugins. The
// observability slug stays org-derived; plugin slugs are scoped within a
// marketplace, so the same slug in two differently-named marketplaces does not
// collide.
//
// Because names are project-scoped, an org's projects now surface as distinct
// marketplaces instead of collapsing. Projects can still share a name (e.g. two
// overrides set to the same value), and a marketplace.json name is a single
// identifier that can't coexist twice on the device — so same-named rows still
// collapse to one, keeping the first row's token. The query orders by pr.id so
// that first row is the org's default project (oldest, lowest id) rather than an
// arbitrary alphabetically-first one.
//
// marketplaceURL constructs the public marketplace git URL from a token; the
// caller owns the URL shape so this builder stays free of server-side config.
func BuildAgentPluginsView(rows []repo.GetAgentPluginSetRow, marketplaceURL func(token string) string) *gen.GetPluginsResult {
	var marketplaces []*gen.AgentMarketplace
	var plugins []*gen.AgentPlugin
	// marketplace name -> the project that owns it (the first/lowest-pr.id row to
	// claim that name). Used to drop assigned plugins from projects whose name
	// collided and collapsed: their plugins live in a different repo than the one
	// served under this name, so emitting them would reference a marketplace that
	// doesn't contain them.
	marketplaceOwner := make(map[string]uuid.UUID)
	etag := sha256.New()

	for _, row := range rows {
		if !row.MarketplaceToken.Valid || row.MarketplaceToken.String == "" {
			continue
		}

		// The per-project override, when set, is the name the project actually
		// published under (UpdateMarketplaceSettings republishes on change), so
		// prefer it over the org-derived default.
		name := naming.MarketplaceName(row.OrganizationName, row.ProjectSlug, row.IsDefaultProject)
		if row.MarketplaceNameOverride.Valid && row.MarketplaceNameOverride.String != "" {
			name = row.MarketplaceNameOverride.String
		}
		owner, seen := marketplaceOwner[name]
		if !seen {
			owner = row.ProjectID
			marketplaceOwner[name] = owner
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

		// Assigned plugin for this project, if the LEFT JOIN matched one — but
		// only when this row's project owns the marketplace under this name. A
		// collapsed (losing) project's marketplace isn't served, so its plugins
		// would reference a repo that doesn't contain them.
		if row.ProjectID == owner && row.PluginID.Valid && row.PluginSlug.Valid {
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
