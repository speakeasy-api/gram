package roledistribution_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestRoleDistributionPipeline_ProjectlessOrganizationAcknowledges(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	_, err := projectsrepo.New(f.db).DeleteProject(ctx, f.project)
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "projectless"}), "a projectless organization must not retry")
	count, err := testrepo.New(f.db).PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestPublishFirstProject(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	publish := func(projectID uuid.UUID) int64 {
		t.Helper()
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: PublishFirstProject runs inside the caller's project creation transaction.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		before, err := testrepo.New(tx).PipelineCountRoleDistributionOutbox(ctx)
		require.NoError(t, err)
		require.NoError(t, requests.PublishFirstProject(ctx, tx, org, projectID))
		after, err := testrepo.New(tx).PipelineCountRoleDistributionOutbox(ctx)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		return after - before
	}
	require.Zero(t, publish(uuid.New()), "an organization with another active project needs no bootstrap")
	_, err := projectsrepo.New(f.db).DeleteProject(ctx, f.project)
	require.NoError(t, err)
	require.EqualValues(t, 1, publish(uuid.New()), "the first active project starts one organization bootstrap")
}
