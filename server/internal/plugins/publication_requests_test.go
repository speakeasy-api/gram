package plugins_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestSignalPluginPublishAfterRequest(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		outcome    plugins.ProjectPublicationRequestOutcome
		wantSignal bool
	}{
		{name: "enqueued", outcome: plugins.ProjectPublicationEnqueued},
		{name: "emission disabled", outcome: plugins.ProjectPublicationEmissionDisabled, wantSignal: true},
		{name: "not configured", outcome: plugins.ProjectPublicationNotConfigured, wantSignal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			signaler := &capturePublishSignaler{}
			err := plugins.SignalPluginPublishAfterRequest(context.Background(), signaler, test.outcome, uuid.New(), "actor")
			require.NoError(t, err)
			if test.wantSignal {
				require.Len(t, signaler.captured(), 1)
			} else {
				require.Empty(t, signaler.captured())
			}
		})
	}
}

func TestPublicationRequestsProjectRequiresExistingMarketplace(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()

	requester := plugins.PublicationRequests{Enabled: true}
	outcome, err := requester.ProjectWithOutcome(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID)
	require.NoError(t, err)
	require.Equal(t, plugins.ProjectPublicationNotConfigured, outcome)
	count, err := testrepo.New(tx).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, requester.Project(ctx, tx, "another-organization", *ac.ProjectID, ac.UserID))
	count, err = testrepo.New(tx).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Error(t, requester.Project(ctx, tx, ac.ActiveOrganizationID, uuid.Nil, ac.UserID))
	outcome, err = (plugins.PublicationRequests{Enabled: false}).ProjectWithOutcome(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID)
	require.NoError(t, err)
	require.Equal(t, plugins.ProjectPublicationEmissionDisabled, outcome)
}

func TestPublicationRequestsOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx, err := ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()

	requester := plugins.PublicationRequests{Enabled: true}
	require.Error(t, requester.Organization(ctx, tx, "", ac.UserID))
	require.NoError(t, requester.Organization(ctx, tx, ac.ActiveOrganizationID, ac.UserID))
	rows, err := testrepo.New(tx).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	var published publicationv1.OrganizationPublicationRequested
	found := false
	for _, row := range rows {
		if row.Topic != "gram.plugins.v1.OrganizationPublicationRequested" {
			continue
		}
		require.False(t, found)
		found = true
		require.Equal(t, ac.ActiveOrganizationID, row.OrganizationID)
		require.NoError(t, proto.Unmarshal(row.Message, &published))
	}
	require.True(t, found)
	require.Equal(t, ac.ActiveOrganizationID, published.GetOrganizationId())
	require.Equal(t, ac.UserID, published.GetCreatedByUserId())
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
	outcome, err := requester.ProjectWithOutcome(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID)
	require.NoError(t, err)
	require.Equal(t, plugins.ProjectPublicationEnqueued, outcome)
	assertCount(0)
	require.NoError(t, tx.Rollback(ctx))
	assertCount(0)

	tx, err = ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	require.NoError(t, requester.Project(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID))
	require.NoError(t, tx.Commit(ctx))
	assertCount(1)
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	var published publicationv1.PublicationRequested
	found := false
	for _, row := range rows {
		if row.Topic != "gram.plugins.v1.PublicationRequested" {
			continue
		}
		require.False(t, found)
		found = true
		require.Equal(t, ac.ActiveOrganizationID, row.OrganizationID)
		require.NoError(t, proto.Unmarshal(row.Message, &published))
	}
	require.True(t, found)
	require.Equal(t, ac.ActiveOrganizationID, published.GetOrganizationId())
	require.Equal(t, ac.ProjectID.String(), published.GetProjectId())
	require.Equal(t, ac.UserID, published.GetCreatedByUserId())

	tx, err = ti.conn.Begin(ctx) //nolint:glint // transaction boundary for SQLc outbox checks
	require.NoError(t, err)
	require.NoError(t, requester.Project(ctx, tx, "another-organization", *ac.ProjectID, ac.UserID))
	require.NoError(t, tx.Commit(ctx))
	assertCount(1)
}
