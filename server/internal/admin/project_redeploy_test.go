package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

type recordingProjectRedeployer struct {
	cloneID     uuid.UUID
	prepared    []deployments.StaffRedeploy
	dispatched  [][3]uuid.UUID
	dispatchErr error
}

func (r *recordingProjectRedeployer) Prepare(_ context.Context, _ pgx.Tx, req deployments.StaffRedeploy) (*types.Deployment, error) {
	r.prepared = append(r.prepared, req)
	return &types.Deployment{ID: r.cloneID.String()}, nil
}

func (r *recordingProjectRedeployer) Dispatch(_ context.Context, projectID, deploymentID, expectedActive uuid.UUID) error {
	r.dispatched = append(r.dispatched, [3]uuid.UUID{projectID, deploymentID, expectedActive})
	return r.dispatchErr
}

func seedCompletedDeployment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	q := deploymentsrepo.New(conn)
	id, err := q.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "user_seed",
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, q.CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{DeploymentID: id, Status: "completed"}))
	return id
}

func TestGetProjectRedeployPreview_ReturnsActiveDeployment(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")
	seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	active := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)

	got, err := svc.GetProjectRedeployPreview(ctx, &gen.GetProjectRedeployPreviewPayload{OrganizationID: "org_one", ProjectID: projectID.String()})
	require.NoError(t, err)
	require.Equal(t, active.String(), got.ActiveDeploymentID)
	require.Equal(t, "default", got.ProjectName)
	require.Zero(t, got.ExternalMcps)
}

func TestGetProject_ReportsActiveDeployment(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")

	empty, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: projectID.String()})
	require.NoError(t, err)
	require.Nil(t, empty.ActiveDeploymentID)

	active := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	got, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: projectID.String()})
	require.NoError(t, err)
	require.NotNil(t, got.ActiveDeploymentID)
	require.Equal(t, active.String(), *got.ActiveDeploymentID)
}

func TestRedeployProject_ClonesTheActiveDeploymentThenDispatches(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")
	active := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	redeployer := &recordingProjectRedeployer{cloneID: uuid.New()}
	svc.SetProjectRedeployer(redeployer)

	got, err := svc.RedeployProject(ctx, &gen.RedeployProjectPayload{OrganizationID: "org_one", ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()})
	require.NoError(t, err)
	require.Equal(t, redeployer.cloneID.String(), got.DeploymentID)
	require.Equal(t, active.String(), got.SourceDeploymentID)
	require.Len(t, redeployer.prepared, 1)
	require.Equal(t, active, redeployer.prepared[0].SourceDeploymentID)
	require.Equal(t, [][3]uuid.UUID{{projectID, redeployer.cloneID, active}}, redeployer.dispatched)
}

func TestRedeployProject_RefusesWhenTheActiveDeploymentMoved(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")
	reviewed := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	redeployer := &recordingProjectRedeployer{cloneID: uuid.New()}
	svc.SetProjectRedeployer(redeployer)

	_, err := svc.RedeployProject(ctx, &gen.RedeployProjectPayload{OrganizationID: "org_one", ProjectID: projectID.String(), ExpectedActiveDeploymentID: reviewed.String()})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, redeployer.prepared)
}

func TestRedeployProject_RefusesWhileANewerDeploymentIsProcessing(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")
	active := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	q := deploymentsrepo.New(conn)
	processing, err := q.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{ProjectID: projectID, OrganizationID: "org_one", UserID: "user_seed", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	require.NoError(t, q.CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{DeploymentID: processing, Status: "pending"}))
	redeployer := &recordingProjectRedeployer{cloneID: uuid.New()}
	svc.SetProjectRedeployer(redeployer)

	_, err = svc.RedeployProject(ctx, &gen.RedeployProjectPayload{OrganizationID: "org_one", ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, redeployer.prepared)
	require.Empty(t, redeployer.dispatched)
}

func TestRedeployProject_RefusesProjectsOutOfScope(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_two", name: "Two", slug: "two"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")
	active := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	undeployed := seedProject(t, ctx, conn, "org_one", "undeployed")
	deleted := seedProject(t, ctx, conn, "org_one", "deleted")
	deletedActive := seedCompletedDeployment(t, ctx, conn, "org_one", deleted)
	_, err := projectsRepo.New(conn).DeleteProject(ctx, deleted)
	require.NoError(t, err)
	redeployer := &recordingProjectRedeployer{cloneID: uuid.New()}
	svc.SetProjectRedeployer(redeployer)

	_, err = svc.RedeployProject(ctx, &gen.RedeployProjectPayload{OrganizationID: "org_two", ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = svc.RedeployProject(ctx, &gen.RedeployProjectPayload{OrganizationID: "org_one", ProjectID: deleted.String(), ExpectedActiveDeploymentID: deletedActive.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = svc.RedeployProject(ctx, &gen.RedeployProjectPayload{OrganizationID: "org_one", ProjectID: undeployed.String(), ExpectedActiveDeploymentID: active.String()})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, redeployer.prepared)
}

func TestRedeployProject_ReportsDispatchFailureAndUnavailableServer(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "default")
	active := seedCompletedDeployment(t, ctx, conn, "org_one", projectID)
	payload := &gen.RedeployProjectPayload{OrganizationID: "org_one", ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()}

	_, err := svc.RedeployProject(ctx, payload)
	requireOopsCode(t, err, oops.CodeUnavailable)

	svc.SetProjectRedeployer(&recordingProjectRedeployer{cloneID: uuid.New(), dispatchErr: errors.New("temporal down")})
	_, err = svc.RedeployProject(ctx, payload)
	requireOopsCode(t, err, oops.CodeUnexpected)
}
