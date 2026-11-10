package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestTransitionDeploymentCompleted(t *testing.T) {
	t.Parallel()

	db, projectID, organizationID := setupTransitionDeploymentTest(t, "ordinary")
	deploymentID := createPendingDeployment(t, db, organizationID, projectID)

	result, err := activities.NewTransitionDeployment(testenv.NewLogger(t), db).Do(t.Context(), projectID, deploymentID, "completed")
	require.NoError(t, err)
	require.True(t, result.Moved)
	require.Equal(t, "completed", result.Status)

	activeID, err := deploymentsrepo.New(db).GetActiveDeploymentID(t.Context(), projectID)
	require.NoError(t, err)
	require.Equal(t, deploymentID, activeID)
}

func TestCompleteDeploymentWithExpectedNoActive(t *testing.T) {
	t.Parallel()

	db, projectID, organizationID := setupTransitionDeploymentTest(t, "none")
	deploymentID := createPendingDeployment(t, db, organizationID, projectID)

	result, err := activities.NewTransitionDeployment(testenv.NewLogger(t), db).CompleteWithExpectedActive(t.Context(), projectID, deploymentID, uuid.Nil)
	require.NoError(t, err)
	require.True(t, result.Moved)
	require.Equal(t, "completed", result.Status)
}

func TestCompleteDeploymentWithExpectedActiveReplaysCompleted(t *testing.T) {
	t.Parallel()

	db, projectID, organizationID := setupTransitionDeploymentTest(t, "unchanged")
	activeID := createPendingDeployment(t, db, organizationID, projectID)
	completeDeploymentFixture(t, db, projectID, activeID)
	deploymentID := createPendingDeployment(t, db, organizationID, projectID)

	transitioner := activities.NewTransitionDeployment(testenv.NewLogger(t), db)
	result, err := transitioner.CompleteWithExpectedActive(t.Context(), projectID, deploymentID, activeID)
	require.NoError(t, err)
	require.True(t, result.Moved)
	require.Equal(t, "completed", result.Status)

	replayed, err := transitioner.CompleteWithExpectedActive(t.Context(), projectID, deploymentID, activeID)
	require.NoError(t, err)
	require.True(t, replayed.Moved)
	require.Equal(t, "completed", replayed.Status)
}

func TestCompleteDeploymentWithStaleExpectedActiveReplaysFailed(t *testing.T) {
	t.Parallel()

	db, projectID, organizationID := setupTransitionDeploymentTest(t, "stale")
	expectedActiveID := createPendingDeployment(t, db, organizationID, projectID)
	completeDeploymentFixture(t, db, projectID, expectedActiveID)
	deploymentID := createPendingDeployment(t, db, organizationID, projectID)
	newActiveID := createPendingDeployment(t, db, organizationID, projectID)
	completeDeploymentFixture(t, db, projectID, newActiveID)

	transitioner := activities.NewTransitionDeployment(testenv.NewLogger(t), db)
	result, err := transitioner.CompleteWithExpectedActive(t.Context(), projectID, deploymentID, expectedActiveID)
	require.NoError(t, err)
	require.True(t, result.Moved)
	require.Equal(t, "failed", result.Status)

	replayed, err := transitioner.CompleteWithExpectedActive(t.Context(), projectID, deploymentID, expectedActiveID)
	require.NoError(t, err)
	require.True(t, replayed.Moved)
	require.Equal(t, "failed", replayed.Status)

	activeID, err := deploymentsrepo.New(db).GetActiveDeploymentID(t.Context(), projectID)
	require.NoError(t, err)
	require.Equal(t, newActiveID, activeID)

	logs, err := deploymentsrepo.New(db).GetDeploymentLogs(t.Context(), deploymentsrepo.GetDeploymentLogsParams{DeploymentID: deploymentID, ProjectID: projectID, CursorSeq: pgtype.Int8{Int64: 0, Valid: false}})
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	require.Contains(t, logs[len(logs)-1].Message, "active deployment changed")
}

func setupTransitionDeploymentTest(t *testing.T, suffix string) (*pgxpool.Pool, uuid.UUID, string) {
	t.Helper()

	db, err := infra.CloneTestDatabase(t, "transition_deployment_"+suffix)
	require.NoError(t, err)

	organizationID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Org",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return db, project.ID, organizationID
}

func createPendingDeployment(t *testing.T, db *pgxpool.Pool, organizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	queries := deploymentsrepo.New(db)
	idempotencyKey := "test-" + uuid.NewString()
	_, err := queries.CreateDeployment(t.Context(), deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: idempotencyKey,
		UserID:         "test-user",
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	deployment, err := queries.GetDeploymentByIdempotencyKey(t.Context(), deploymentsrepo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	for _, status := range []string{"created", "pending"} {
		state, err := queries.TransitionDeployment(t.Context(), deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID,
			Status:       status,
			ProjectID:    projectID,
			Event:        "test",
			Message:      "test deployment status",
		})
		require.NoError(t, err)
		require.True(t, state.Moved)
	}

	return deployment.Deployment.ID
}

func completeDeploymentFixture(t *testing.T, db *pgxpool.Pool, projectID uuid.UUID, deploymentID uuid.UUID) {
	t.Helper()

	state, err := deploymentsrepo.New(db).TransitionDeployment(t.Context(), deploymentsrepo.TransitionDeploymentParams{
		DeploymentID: deploymentID,
		Status:       "completed",
		ProjectID:    projectID,
		Event:        "test",
		Message:      "test deployment status",
	})
	require.NoError(t, err)
	require.True(t, state.Moved)
}

func TestGuardedCompletionSerializesWithLegacyWorkerStatement(t *testing.T) {
	t.Parallel()
	db, projectID, organizationID := setupTransitionDeploymentTest(t, "legacy_lock")
	originalID := createPendingDeployment(t, db, organizationID, projectID)
	completeDeploymentFixture(t, db, projectID, originalID)
	legacyID := createPendingDeployment(t, db, organizationID, projectID)
	guardedID := createPendingDeployment(t, db, organizationID, projectID)

	tx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the project transaction lock is the behavior under test.
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = projectsrepo.New(tx).GetProjectByIDForUpdate(t.Context(), projectID)
	require.NoError(t, err)

	// This is the exact pre-rollout SQLc statement, without the new activity's
	// explicit project lock. Its log's project FK still needs KEY SHARE, so it
	// cannot commit while guarded promotion holds FOR UPDATE.
	legacyCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = deploymentsrepo.New(db).TransitionDeployment(legacyCtx, deploymentsrepo.TransitionDeploymentParams{DeploymentID: legacyID, ProjectID: projectID, Status: "completed", Event: "deployment:status_change", Message: "legacy completion"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	activeID, err := deploymentsrepo.New(db).GetActiveDeploymentID(t.Context(), projectID)
	require.NoError(t, err)
	require.Equal(t, originalID, activeID)
	require.NoError(t, tx.Rollback(t.Context()))

	legacy, err := deploymentsrepo.New(db).TransitionDeployment(t.Context(), deploymentsrepo.TransitionDeploymentParams{DeploymentID: legacyID, ProjectID: projectID, Status: "completed", Event: "deployment:status_change", Message: "legacy completion"})
	require.NoError(t, err)
	require.Equal(t, "completed", legacy.Status)
	result, err := activities.NewTransitionDeployment(testenv.NewLogger(t), db).CompleteWithExpectedActive(t.Context(), projectID, guardedID, originalID)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	activeID, err = deploymentsrepo.New(db).GetActiveDeploymentID(t.Context(), projectID)
	require.NoError(t, err)
	require.Equal(t, legacyID, activeID)
}
