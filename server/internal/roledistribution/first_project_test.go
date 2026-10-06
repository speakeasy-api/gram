package roledistribution_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// createProjectAndPublish creates a project and runs PublishFirstProject in
// one transaction, as project creation does, and returns the new outbox rows.
func createProjectAndPublish(ctx context.Context, t *testing.T, tx pgx.Tx, org, slug string) int64 {
	t.Helper()
	before, err := testrepo.New(tx).PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	projectID := uuid.New()
	_, err = testrepo.New(tx).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: projectID, OrganizationID: org, Name: slug, Slug: slug})
	require.NoError(t, err)
	require.NoError(t, requests.PublishFirstProject(ctx, tx, org, projectID))
	after, err := testrepo.New(tx).PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	return after - before
}

func TestPublishFirstProject(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	publish := func(slug string) int64 {
		t.Helper()
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: PublishFirstProject runs inside the caller's project creation transaction.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		published := createProjectAndPublish(ctx, t, tx, org, slug)
		require.NoError(t, tx.Commit(ctx))
		return published
	}
	require.Zero(t, publish("second"), "an organization with another active project needs no bootstrap")
	_, err := projectsrepo.New(f.db).DeleteProject(ctx, f.project)
	require.NoError(t, err)
	require.Zero(t, publish("third"), "the second project is still active")

	f = newPipelineFixture(t)
	org = f.event.GetOrganizationId()
	_, err = projectsrepo.New(f.db).DeleteProject(ctx, f.project)
	require.NoError(t, err)
	require.EqualValues(t, 1, publish("first"), "the first active project starts one organization bootstrap")
}

func TestPublishFirstProject_WaitsForConcurrentDelete(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	deletion, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: Hold a project delete open while another project is created.
	require.NoError(t, err)
	defer func() { _ = deletion.Rollback(context.WithoutCancel(ctx)) }()
	_, err = projectsrepo.New(deletion).DeleteProject(ctx, f.project)
	require.NoError(t, err)
	creation, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: Concurrent project creation must wait for the delete decision.
	require.NoError(t, err)
	defer func() { _ = creation.Rollback(context.WithoutCancel(ctx)) }()
	result := make(chan int64, 1)
	go func() { result <- createProjectAndPublish(ctx, t, creation, org, "replacement") }()
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(f.db).RolloutBlockedBackends(ctx, int32(deletion.Conn().PgConn().PID()))
		return err == nil && len(blocked) > 0
	}, 5*time.Second, 10*time.Millisecond, "creation must wait on the in-flight delete")
	require.NoError(t, deletion.Commit(ctx))
	select {
	case published := <-result:
		require.EqualValues(t, 1, published, "the replacement is the only active project once the delete commits")
	case <-ctx.Done():
		t.Fatal("creation did not finish:", ctx.Err())
	}
}
