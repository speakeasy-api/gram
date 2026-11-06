package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// fakeProjectRedeployer records a created deployment in the write's
// transaction, as the real clone does, without running any sources.
type fakeProjectRedeployer struct {
	mu          sync.Mutex
	prepared    []deployments.StaffRedeploy
	surface     string
	dispatched  [][3]uuid.UUID
	dispatchErr error
}

func (r *fakeProjectRedeployer) Prepare(ctx context.Context, tx pgx.Tx, req deployments.StaffRedeploy) (*types.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prepared = append(r.prepared, req)
	r.surface, _ = contextvalues.GetActingSurface(ctx)
	q := deploymentsrepo.New(tx)
	id, err := q.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{ProjectID: req.ProjectID, OrganizationID: req.OrganizationID, UserID: "user_seed", IdempotencyKey: uuid.NewString()})
	if err != nil {
		return nil, fmt.Errorf("insert fake clone: %w", err)
	}
	if err := q.CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{DeploymentID: id, Status: "created"}); err != nil {
		return nil, fmt.Errorf("record fake clone status: %w", err)
	}
	return &types.Deployment{ID: id.String()}, nil
}

func (r *fakeProjectRedeployer) Dispatch(_ context.Context, projectID, deploymentID, expectedActive uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dispatched = append(r.dispatched, [3]uuid.UUID{projectID, deploymentID, expectedActive})
	return r.dispatchErr
}

func newProjectRedeployWriter(f writeFixture, redeployer *fakeProjectRedeployer) *projectRedeployWriter {
	writes := WriteConfig{Enabled: true}
	return &projectRedeployWriter{runner: f.runner, redeployer: redeployer, writes: writes, now: time.Now}
}

func seedRedeployProject(t *testing.T, f writeFixture, organizationID, slug string) uuid.UUID {
	t.Helper()
	project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Project " + slug, Slug: slug, OrganizationID: organizationID})
	require.NoError(t, err)
	return project.ID
}

func seedRedeployDeployment(t *testing.T, f writeFixture, organizationID string, projectID uuid.UUID, status string) uuid.UUID {
	t.Helper()
	q := deploymentsrepo.New(f.db)
	id, err := q.InsertDeployment(t.Context(), deploymentsrepo.InsertDeploymentParams{ProjectID: projectID, OrganizationID: organizationID, UserID: "user_seed", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	require.NoError(t, q.CreateDeploymentStatus(t.Context(), deploymentsrepo.CreateDeploymentStatusParams{DeploymentID: id, Status: status}))
	return id
}

func TestProjectRedeployApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy")
	redeployer := &fakeProjectRedeployer{}
	writer := newProjectRedeployWriter(f, redeployer)
	ctx := writeContext(t, f)
	projectID := seedRedeployProject(t, f, f.orgA, "redeploy")
	active := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")

	receipt, err := writer.apply(ctx, RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()})
	require.NoError(t, err)
	require.Equal(t, active.String(), receipt.SourceDeploymentID)
	require.NotEqual(t, active.String(), receipt.DeploymentID)
	require.Equal(t, "Project redeploy", receipt.ProjectName)
	require.Contains(t, receipt.SideEffects, "External MCP sources are not carried over")

	require.Len(t, redeployer.prepared, 1)
	staff, ok := contextvalues.GetAdminAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, staff.OIDCSubject, redeployer.prepared[0].Actor.ID)
	require.Equal(t, string(audit.SurfaceAdminMCP), redeployer.surface)
	require.Equal(t, [][3]uuid.UUID{{projectID, uuid.MustParse(receipt.DeploymentID), active}}, redeployer.dispatched)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationRedeployProject, f.orgA, "executed"))
}

func TestProjectRedeployRejectsInexactOrForeignTargets(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_targets")
	redeployer := &fakeProjectRedeployer{}
	writer := newProjectRedeployWriter(f, redeployer)
	ctx := writeContext(t, f)
	projectID := seedRedeployProject(t, f, f.orgA, "targets")
	active := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")

	for _, input := range []RedeployProjectInput{
		{OrganizationID: "synthetic-a", ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()},
		{OrganizationID: f.orgA, ProjectID: "targets", ExpectedActiveDeploymentID: active.String()},
		{OrganizationID: f.orgA, ProjectID: projectID.String()},
		{OrganizationID: f.orgB, ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()},
	} {
		_, err := writer.apply(ctx, input)
		require.Error(t, err)
	}
	require.Empty(t, redeployer.prepared)
	require.Empty(t, redeployer.dispatched)
}

func TestProjectRedeployRefusesWhenTheActiveDeploymentMoved(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_stale")
	redeployer := &fakeProjectRedeployer{}
	writer := newProjectRedeployWriter(f, redeployer)
	projectID := seedRedeployProject(t, f, f.orgA, "stale")
	confirmed := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")
	seedRedeployDeployment(t, f, f.orgA, projectID, "completed")

	_, err := writer.apply(writeContext(t, f), RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: confirmed.String()})
	require.ErrorIs(t, err, ErrStaleState)
	require.Empty(t, redeployer.prepared)
	require.Empty(t, redeployer.dispatched)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationRedeployProject, f.orgA, "failed"))
}

func TestProjectRedeployRefusesProjectWithNoActiveDeployment(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_empty")
	writer := newProjectRedeployWriter(f, &fakeProjectRedeployer{})
	projectID := seedRedeployProject(t, f, f.orgA, "empty")

	_, err := writer.apply(writeContext(t, f), RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: uuid.NewString()})
	require.ErrorContains(t, err, "no active deployment")
}

func TestProjectRedeployRetryDoesNotCloneAgain(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_retry")
	redeployer := &fakeProjectRedeployer{}
	writer := newProjectRedeployWriter(f, redeployer)
	ctx := writeContext(t, f)
	projectID := seedRedeployProject(t, f, f.orgA, "retry")
	active := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")
	input := RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()}

	_, err := writer.apply(ctx, input)
	require.NoError(t, err)
	_, err = writer.apply(ctx, input)
	require.ErrorIs(t, err, admin.ErrProjectRedeployInProgress)
	require.Len(t, redeployer.prepared, 1)
	require.Len(t, redeployer.dispatched, 1)

	writer.now = func() time.Time { return time.Now().Add(background.ProcessDeploymentRunTimeout + time.Minute) }
	_, err = writer.apply(ctx, input)
	require.NoError(t, err, "a deployment past the workflow timeout is not processing and must not block the project")
	require.Len(t, redeployer.prepared, 2)
}

func TestProjectRedeployConcurrentCallsCloneOnce(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_concurrent")
	redeployer := &fakeProjectRedeployer{}
	writer := newProjectRedeployWriter(f, redeployer)
	ctx := writeContext(t, f)
	projectID := seedRedeployProject(t, f, f.orgA, "concurrent")
	active := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")
	input := RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()}

	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			_, err := writer.apply(ctx, input)
			results <- err
		})
	}
	group.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, admin.ErrProjectRedeployInProgress)
	}
	require.Equal(t, 1, succeeded)
	require.Len(t, redeployer.prepared, 1)
}

func TestProjectRedeployIgnoresFinishedNewerDeployments(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_failed")
	writer := newProjectRedeployWriter(f, &fakeProjectRedeployer{})
	projectID := seedRedeployProject(t, f, f.orgA, "failed")
	active := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")
	seedRedeployDeployment(t, f, f.orgA, projectID, "failed")

	_, err := writer.apply(writeContext(t, f), RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()})
	require.NoError(t, err)
}

func TestProjectRedeployReportsAStartFailureNamingTheDeployment(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_dispatch")
	redeployer := &fakeProjectRedeployer{dispatchErr: errors.New("temporal down")}
	writer := newProjectRedeployWriter(f, redeployer)
	projectID := seedRedeployProject(t, f, f.orgA, "dispatch")
	active := seedRedeployDeployment(t, f, f.orgA, projectID, "completed")

	_, err := writer.apply(writeContext(t, f), RedeployProjectInput{OrganizationID: f.orgA, ProjectID: projectID.String(), ExpectedActiveDeploymentID: active.String()})
	require.ErrorContains(t, err, "did not start")
	require.ErrorContains(t, err, redeployer.dispatched[0][1].String())
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationRedeployProject, f.orgA, "executed"), "the clone committed")
}

func TestProjectRedeployRequiresWritableLiveStaff(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_project_redeploy_authority")
	writer := newProjectRedeployWriter(f, &fakeProjectRedeployer{})
	ctx := writeContext(t, f)
	principal, ok := principalFromContext(ctx)
	require.True(t, ok)
	principal.Scopes = []string{ScopeRead}
	_, err := writer.apply(context.WithValue(ctx, principalKey{}, principal), RedeployProjectInput{})
	require.ErrorIs(t, err, ErrWriteScope)
	writer.writes.Enabled = false
	_, err = writer.apply(ctx, RedeployProjectInput{})
	require.ErrorIs(t, err, ErrWriteDisabled)
}
