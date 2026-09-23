package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestPublicationRequestsProjectRequiresExistingMarketplace(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()

	requester := plugins.PublicationRequests{Enabled: true}
	require.NoError(t, requester.Project(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID))
	count, err := testrepo.New(tx).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, requester.Project(ctx, tx, "another-organization", *ac.ProjectID, ac.UserID))
	count, err = testrepo.New(tx).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Error(t, requester.Project(ctx, tx, ac.ActiveOrganizationID, uuid.Nil, ac.UserID))
}

func TestPublicationRequestsCommitAndRollbackWithMutation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsServiceWithGitHub(t, &mockGitHubPublisher{})
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Existing marketplace"})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	requester := plugins.PublicationRequests{Enabled: true}
	baseline, err := testrepo.New(ti.conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	assertCount := func(additional int64) {
		t.Helper()
		count, err := testrepo.New(ti.conn).CountPublishOutboxRows(ctx)
		require.NoError(t, err)
		require.Equal(t, baseline+additional, count)
	}

	tx, err := ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	require.NoError(t, requester.Project(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID))
	assertCount(0)
	require.NoError(t, tx.Rollback(ctx))
	assertCount(0)

	tx, err = ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	require.NoError(t, requester.Project(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID))
	require.NoError(t, tx.Commit(ctx))
	assertCount(1)

	tx, err = ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	require.NoError(t, requester.Project(ctx, tx, "another-organization", *ac.ProjectID, ac.UserID))
	require.NoError(t, tx.Commit(ctx))
	assertCount(1)
}
