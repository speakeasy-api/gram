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

func TestBuildAgentPluginsView_GroupsPluginsByProjectMarketplace(t *testing.T) {
	t.Parallel()

	projectA := uuid.New()
	projectB := uuid.New()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	rows := []repo.GetAssignedPluginsForAgentRow{
		{
			PluginID:             uuid.New(),
			PluginSlug:           "engineering-tools",
			PluginUpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			OrganizationID:       "org_acme",
			ProjectID:            projectA,
			OrganizationSlug:     "acme",
			ProjectSlug:          "default",
			MarketplaceToken:     pgtype.Text{String: "tokA", Valid: true},
			MarketplaceUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
		{
			PluginID:             uuid.New(),
			PluginSlug:           "speakeasy-observability",
			PluginUpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			OrganizationID:       "org_acme",
			ProjectID:            projectA,
			OrganizationSlug:     "acme",
			ProjectSlug:          "default",
			MarketplaceToken:     pgtype.Text{String: "tokA", Valid: true},
			MarketplaceUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
		{
			PluginID:             uuid.New(),
			PluginSlug:           "sales-tools",
			PluginUpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			OrganizationID:       "org_acme",
			ProjectID:            projectB,
			OrganizationSlug:     "acme",
			ProjectSlug:          "sales",
			MarketplaceToken:     pgtype.Text{String: "tokB", Valid: true},
			MarketplaceUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.NotEmpty(t, result.Etag)
	require.Len(t, result.Marketplaces, 2, "one marketplace per distinct project")
	require.Equal(t, "speakeasy-acme-default", result.Marketplaces[0].Name)
	require.Equal(t, "https://app.getgram.ai/marketplace/tokA.git", result.Marketplaces[0].URL)
	require.True(t, result.Marketplaces[0].AutoUpdate)
	require.Equal(t, "speakeasy-acme-sales", result.Marketplaces[1].Name)
	require.Equal(t, "https://app.getgram.ai/marketplace/tokB.git", result.Marketplaces[1].URL)

	require.Len(t, result.Plugins, 3)
	require.Equal(t, "engineering-tools", result.Plugins[0].Slug)
	require.Equal(t, "speakeasy-acme-default", result.Plugins[0].MarketplaceName)
	require.Equal(t, "speakeasy-observability", result.Plugins[1].Slug)
	require.Equal(t, "speakeasy-acme-default", result.Plugins[1].MarketplaceName)
	require.Equal(t, "sales-tools", result.Plugins[2].Slug)
	require.Equal(t, "speakeasy-acme-sales", result.Plugins[2].MarketplaceName)
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

	rows := []repo.GetAssignedPluginsForAgentRow{
		{
			PluginID:         uuid.New(),
			PluginSlug:       "p",
			PluginUpdatedAt:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
			OrganizationID:   "org_acme",
			ProjectID:        uuid.New(),
			OrganizationSlug: "acme",
			ProjectSlug:      "default",
			MarketplaceToken: pgtype.Text{Valid: false},
		},
	}

	result := mv.BuildAgentPluginsView(rows, testMarketplaceURL)

	require.Empty(t, result.Marketplaces)
	require.Empty(t, result.Plugins)
}

func TestBuildAgentPluginsView_ETagStableAcrossCalls(t *testing.T) {
	t.Parallel()

	row := makeAgentRow("p", "tok", time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC))

	first := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsForAgentRow{row}, testMarketplaceURL).Etag
	second := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsForAgentRow{row}, testMarketplaceURL).Etag

	require.Equal(t, first, second)
}

func TestBuildAgentPluginsView_ETagChangesWhenPluginUpdated(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	before := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsForAgentRow{makeAgentRow("p", "tok", t0)}, testMarketplaceURL).Etag
	after := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsForAgentRow{makeAgentRow("p", "tok", t0.Add(time.Second))}, testMarketplaceURL).Etag

	require.NotEqual(t, before, after)
}

func TestBuildAgentPluginsView_ETagIgnoresMarketplaceURLPrefix(t *testing.T) {
	t.Parallel()

	row := makeAgentRow("p", "tok", time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC))

	prod := func(token string) string { return "https://app.getgram.ai/marketplace/" + token + ".git" }
	staging := func(token string) string { return "https://staging.getgram.ai/marketplace/" + token + ".git" }

	first := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsForAgentRow{row}, prod).Etag
	second := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsForAgentRow{row}, staging).Etag

	require.Equal(t, first, second, "ETag covers content, not the deployment-config server URL")
}

// makeAgentRow deterministically derives plugin / project UUIDs from (slug,
// token) so two calls with the same arguments produce the same row. ETag
// tests then meaningfully exercise the varying field (e.g. updated_at)
// rather than passing accidentally because random UUIDs differed.
func makeAgentRow(slug, token string, updatedAt time.Time) repo.GetAssignedPluginsForAgentRow {
	return repo.GetAssignedPluginsForAgentRow{
		PluginID:             uuid.NewSHA1(uuid.NameSpaceURL, []byte("plugin:"+slug)),
		PluginSlug:           slug,
		PluginUpdatedAt:      pgtype.Timestamptz{Time: updatedAt, Valid: true},
		OrganizationID:       "org_acme",
		ProjectID:            uuid.NewSHA1(uuid.NameSpaceURL, []byte("project:"+token)),
		OrganizationSlug:     "acme",
		ProjectSlug:          "default",
		MarketplaceToken:     pgtype.Text{String: token, Valid: true},
		MarketplaceUpdatedAt: pgtype.Timestamptz{Time: updatedAt, Valid: true},
	}
}
