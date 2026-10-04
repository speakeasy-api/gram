package plugins_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestPublicationEvidenceReportsLastPushFromConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, ac.ProjectID)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Evidence Push"})
	require.NoError(t, err)

	before, err := ti.service.ResolvePublicationEvidence(ctx, ac.ActiveOrganizationID, *ac.ProjectID, []string{plugin.Slug})
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.True(t, before[0].NotConfigured)
	require.Nil(t, before[0].LastSuccessfulPushAt, "a project that never published has no push time")

	conn, err := pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                *ac.ProjectID,
		InstallationID:           12345,
		RepoOwner:                "test-owner",
		RepoName:                 "test-marketplace",
		MarketplaceToken:         conv.ToPGText("marketplace-token"),
		PublishedMcpFingerprints: []byte(`{}`),
		PublishedHooksVersion:    pgtype.Text{},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)

	after, err := ti.service.ResolvePublicationEvidence(ctx, ac.ActiveOrganizationID, *ac.ProjectID, []string{plugin.Slug})
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.False(t, after[0].NotConfigured)
	require.NotNil(t, after[0].LastSuccessfulPushAt)
	require.True(t, conn.UpdatedAt.Time.Equal(*after[0].LastSuccessfulPushAt))
}
