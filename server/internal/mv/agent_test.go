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
func marketplaceRow(orgSlug, orgName, projectSlug, token string, now time.Time) repo.GetAgentPluginSetRow {
	return repo.GetAgentPluginSetRow{
		ProjectID:            uuid.New(),
		ProjectSlug:          projectSlug,
		OrganizationSlug:     orgSlug,
		OrganizationName:     orgName,
		MarketplaceToken:     pgtype.Text{String: token, Valid: true},
		MarketplaceUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
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
		marketplaceRow("acme", "Acme Corp", "default", "tokA", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 1)
	require.Equal(t, "speakeasy-acme-default", result.Marketplaces[0].Name)

	// Observability must be present even with zero assignments.
	require.Len(t, result.Plugins, 1)
	require.Equal(t, "acme-corp-observability", result.Plugins[0].Slug)
	require.Equal(t, "speakeasy-acme-default", result.Plugins[0].MarketplaceName)
}

func TestBuildAgentPluginsView_ObservabilityPlusAssignments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	base := marketplaceRow("acme", "Acme Corp", "default", "tokA", now)
	rows := []repo.GetAgentPluginSetRow{
		withPlugin(base, "engineering-tools", now),
		withPlugin(base, "sales-tools", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 1)

	slugs := make([]string, 0, len(result.Plugins))
	for _, p := range result.Plugins {
		require.Equal(t, "speakeasy-acme-default", p.MarketplaceName)
		slugs = append(slugs, p.Slug)
	}
	// Observability first (emitted with the marketplace), then the assigned ones.
	require.Equal(t, []string{"acme-corp-observability", "engineering-tools", "sales-tools"}, slugs)
}

func TestBuildAgentPluginsView_MultipleMarketplacesEachGetObservability(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	rows := []repo.GetAgentPluginSetRow{
		marketplaceRow("acme", "Acme Corp", "default", "tokA", now),
		marketplaceRow("acme", "Acme Corp", "sales", "tokB", now),
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Len(t, result.Marketplaces, 2)
	require.Len(t, result.Plugins, 2, "one observability plugin per marketplace")
	require.Equal(t, "speakeasy-acme-default", result.Plugins[0].MarketplaceName)
	require.Equal(t, "speakeasy-acme-sales", result.Plugins[1].MarketplaceName)
	for _, p := range result.Plugins {
		require.Equal(t, "acme-corp-observability", p.Slug)
	}
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
	row := marketplaceRow("acme", "Acme Corp", "default", "", now)
	row.MarketplaceToken = pgtype.Text{Valid: false}

	result := mv.BuildAgentPluginsView([]repo.GetAgentPluginSetRow{row}, testMarketplaceURL)

	require.Empty(t, result.Marketplaces)
	require.Empty(t, result.Plugins)
}

func TestBuildAgentPluginsView_ETagStableAndSensitive(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	rowsAt := func(ts time.Time) []repo.GetAgentPluginSetRow {
		return []repo.GetAgentPluginSetRow{withPlugin(marketplaceRow("acme", "Acme Corp", "default", "tokA", ts), "eng", ts)}
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
