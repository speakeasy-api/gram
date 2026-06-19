package mv_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
)

func testMarketplaceURL(token string) string {
	return "https://app.getgram.ai/marketplace/" + token + ".git"
}

// marketplaceRow is a published-marketplace row with no assigned plugin
// (null plugin columns) — the shape the LEFT JOIN produces when the user has
// no assignment in that project.
func marketplaceRow(orgSlug, orgName, projectSlug string, isDefault bool, token string, now time.Time) repo.GetAgentPluginSetRow {
	return repo.GetAgentPluginSetRow{
		ProjectID:            uuid.New(),
		ProjectSlug:          projectSlug,
		OrganizationSlug:     orgSlug,
		OrganizationName:     orgName,
		MarketplaceToken:     pgtype.Text{String: token, Valid: true},
		MarketplaceUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		IsDefaultProject:     isDefault,
		PluginID:             uuid.NullUUID{},
		PluginSlug:           pgtype.Text{},
		PluginUpdatedAt:      pgtype.Timestamptz{},
	}
}

func withPlugin(row repo.GetAgentPluginSetRow, slug string, now time.Time) repo.GetAgentPluginSetRow {
	row.PluginID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	row.PluginSlug = pgtype.Text{String: slug, Valid: true}
	row.PluginUpdatedAt = pgtype.Timestamptz{Time: now, Valid: true}
	return row
}

func TestBuildAgentPluginsView_ObservabilityWithoutAssignments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	// One published marketplace, user has no assignments → one row, null plugin.
	rows := []repo.GetAgentPluginSetRow{
		marketplaceRow("acme", "Acme Corp", "default", true, "tokA", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 1)
	// Name must equal the published marketplace.json name (naming.MarketplaceName),
	// or Claude Code can't resolve the enabledPlugins entries.
	require.Equal(t, "acme-corp-speakeasy", result.Marketplaces[0].Name)

	// Observability must be present even with zero assignments.
	require.Len(t, result.Plugins, 1)
	require.Equal(t, "acme-corp-observability", result.Plugins[0].Slug)
	require.Equal(t, "acme-corp-speakeasy", result.Plugins[0].MarketplaceName)
}

func TestBuildAgentPluginsView_ObservabilityPlusAssignments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	base := marketplaceRow("acme", "Acme Corp", "default", true, "tokA", now)
	rows := []repo.GetAgentPluginSetRow{
		withPlugin(base, "engineering-tools", now),
		withPlugin(base, "sales-tools", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 1)

	slugs := make([]string, 0, len(result.Plugins))
	for _, p := range result.Plugins {
		require.Equal(t, "acme-corp-speakeasy", p.MarketplaceName)
		slugs = append(slugs, p.Slug)
	}
	// Observability first (emitted with the marketplace), then the assigned ones.
	require.Equal(t, []string{"acme-corp-observability", "engineering-tools", "sales-tools"}, slugs)
}

func TestBuildAgentPluginsView_MultipleProjectsYieldDistinctMarketplaces(t *testing.T) {
	t.Parallel()

	// Names are project-scoped: the default project keeps the bare org name, the
	// non-default project is suffixed by its slug. So two published projects in
	// the same org surface as two distinct marketplaces, each with its own token.
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	rows := []repo.GetAgentPluginSetRow{
		marketplaceRow("acme", "Acme Corp", "default", true, "tokA", now),
		marketplaceRow("acme", "Acme Corp", "sales", false, "tokB", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 2, "distinct project-scoped names do not collapse")
	byName := map[string]string{}
	for _, m := range result.Marketplaces {
		byName[m.Name] = m.URL
	}
	require.Equal(t, "https://app.getgram.ai/marketplace/tokA.git", byName["acme-corp-speakeasy"], "default project keeps the bare org name")
	require.Equal(t, "https://app.getgram.ai/marketplace/tokB.git", byName["acme-corp-sales-speakeasy"], "non-default project is scoped by slug")

	// Each marketplace gets its own observability plugin, attached to that
	// marketplace's name — not just the right count of plugins.
	obsByMarketplace := map[string]int{}
	for _, p := range result.Plugins {
		require.Equal(t, "acme-corp-observability", p.Slug)
		obsByMarketplace[p.MarketplaceName]++
	}
	require.Equal(t, map[string]int{
		"acme-corp-speakeasy":       1,
		"acme-corp-sales-speakeasy": 1,
	}, obsByMarketplace, "observability must be emitted once per marketplace, against the correct marketplace name")
}

func TestBuildAgentPluginsView_CollapsedProjectPluginsDropped(t *testing.T) {
	t.Parallel()

	// Two projects collide on a name (here both default-flagged, standing in for
	// two equal overrides). The losing project's marketplace isn't served, so its
	// assigned plugins must NOT be emitted — they'd reference the winner's repo,
	// which doesn't contain them.
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	winner := marketplaceRow("acme", "Acme Corp", "default", true, "tokA", now)
	loser := marketplaceRow("acme", "Acme Corp", "other", true, "tokB", now)
	rows := []repo.GetAgentPluginSetRow{
		withPlugin(winner, "winner-plugin", now),
		withPlugin(loser, "loser-plugin", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 1)
	slugs := make([]string, 0, len(result.Plugins))
	for _, p := range result.Plugins {
		require.Equal(t, "acme-corp-speakeasy", p.MarketplaceName)
		slugs = append(slugs, p.Slug)
	}
	require.ElementsMatch(t, []string{"acme-corp-observability", "winner-plugin"}, slugs,
		"the collapsed project's plugin must be dropped, not attached to the winner's marketplace")
	require.NotContains(t, slugs, "loser-plugin")
}

func TestBuildAgentPluginsView_SameNameRowsCollapseToDefault(t *testing.T) {
	t.Parallel()

	// Rows that resolve to the SAME name (here, two rows flagged default — the
	// degenerate case, or two equal overrides) still collapse: a marketplace.json
	// name can't appear twice on the device. The first row (lowest pr.id, ordered
	// by the query) wins, so its token is the one kept.
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	rows := []repo.GetAgentPluginSetRow{
		marketplaceRow("acme", "Acme Corp", "default", true, "tokA", now),
		marketplaceRow("acme", "Acme Corp", "other", true, "tokB", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 1, "same-named rows collapse to one")
	require.Equal(t, "acme-corp-speakeasy", result.Marketplaces[0].Name)
	require.Equal(t, "https://app.getgram.ai/marketplace/tokA.git", result.Marketplaces[0].URL, "first row's token wins")
}

func TestBuildAgentPluginsView_EmptyRows(t *testing.T) {
	t.Parallel()

	result := mv.BuildAgentPluginsView(nil, testMarketplaceURL)

	require.Empty(t, result.Marketplaces)
	require.Empty(t, result.Plugins)
	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", result.Etag)
}

func TestBuildAgentPluginsView_SkipsRowsWithMissingMarketplaceToken(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := marketplaceRow("acme", "Acme Corp", "default", true, "", now)
	row.MarketplaceToken = pgtype.Text{Valid: false}

	result := mv.BuildAgentPluginsView([]repo.GetAgentPluginSetRow{row}, testMarketplaceURL)

	require.Empty(t, result.Marketplaces)
	require.Empty(t, result.Plugins)
}

func TestBuildAgentPluginsView_ETagIgnoresRowsThatDoNotRender(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	base := []repo.GetAgentPluginSetRow{
		marketplaceRow("acme", "Acme Corp", "default", true, "tokA", now),
	}

	skipped := marketplaceRow("acme", "Acme Corp", "empty-token", false, "", now.Add(time.Hour))
	skipped.MarketplaceToken = pgtype.Text{Valid: false}

	// Resolves to the same name as base (also default), so it collapses rather
	// than rendering a second marketplace — the case this test pins.
	collapsed := marketplaceRow("acme", "Acme Corp", "other", true, "tokB", now.Add(2*time.Hour))

	baseResult := mv.BuildAgentPluginsView(base, testMarketplaceURL)
	withSkipped := mv.BuildAgentPluginsView(append(append([]repo.GetAgentPluginSetRow{}, base...), skipped), testMarketplaceURL)
	withCollapsed := mv.BuildAgentPluginsView(append(append([]repo.GetAgentPluginSetRow{}, base...), collapsed), testMarketplaceURL)

	require.Equal(t, baseResult.Etag, withSkipped.Etag, "skipped rows should not affect the sync token")
	require.Equal(t, baseResult.Etag, withCollapsed.Etag, "collapsed marketplace rows should not affect the sync token")
	require.Equal(t, baseResult.Marketplaces, withCollapsed.Marketplaces)
	require.Equal(t, baseResult.Plugins, withCollapsed.Plugins)
}

func TestBuildAgentPluginsView_ETagStableAndSensitive(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	rowsAt := func(ts time.Time) []repo.GetAgentPluginSetRow {
		return []repo.GetAgentPluginSetRow{withPlugin(marketplaceRow("acme", "Acme Corp", "default", true, "tokA", ts), "eng", ts)}
	}

	a := mv.BuildAgentPluginsView(rowsAt(t0), testMarketplaceURL).Etag
	b := mv.BuildAgentPluginsView(rowsAt(t0), testMarketplaceURL).Etag
	require.Equal(t, a, b, "stable for identical input")

	c := mv.BuildAgentPluginsView(rowsAt(t0.Add(time.Second)), testMarketplaceURL).Etag
	require.NotEqual(t, a, c, "changes when a timestamp changes")

	prod := mv.BuildAgentPluginsView(rowsAt(t0), func(tok string) string { return "https://prod/" + tok }).Etag
	staging := mv.BuildAgentPluginsView(rowsAt(t0), func(tok string) string { return "https://staging/" + tok }).Etag
	require.Equal(t, prod, staging, "ETag ignores the deployment-config marketplace URL")
}
