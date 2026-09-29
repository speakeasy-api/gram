package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	provisioningrepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/stretchr/testify/require"
)

func TestPluginAudienceReceiptBeforeAdmissionAllowsReconciliationProjectLock(t *testing.T) {
	t.Parallel()
	testPluginAudienceReceiptProjectLock(t, false)
}

func TestPluginAudienceReceiptBeforeAdmissionAllowsCleanupProjectLock(t *testing.T) {
	t.Parallel()
	testPluginAudienceReceiptProjectLock(t, true)
}

func testPluginAudienceReceiptProjectLock(t *testing.T, cleanup bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_audience_receipt_project_lock")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Audience tools", "audience-tools")
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)

	// Pause the real audience service after its receipt INSERT acquires the
	// project FK KEY SHARE lock, but before its callback gets admission. A
	// reconciler reaches project locking while it already holds admission.
	blocker, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: controlled transaction for deterministic lock interleaving
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return blocker.Rollback(ctx) })
	require.NoError(t, admission.LockProject(ctx, blocker, project.ID))
	blockerPID := int(blocker.Conn().PgConn().PID())
	input := SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.ID.String(), AssignmentReferences: []string{},
		ExpectedAssignmentVersion: before.AssignmentVersion, IdempotencyKey: "receipt-project-lock", Confirmed: true,
	}
	type mutationResult struct {
		output SetPluginAssignmentsOutput
		err    error
	}
	mutated := make(chan mutationResult, 1)
	go func() {
		output, err := service.SetPluginAssignments(ctx, principal, input)
		mutated <- mutationResult{output: output, err: err}
	}()
	// An advisory wait identifies the actual callback, not receipt insertion
	// or any preflight query. Therefore the receipt's FK lock is already held.
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory')`, blockerPID).Scan(&waiting) //nolint:glint // notestingrawsql: observe deterministic receipt callback/admission wait edge
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)

	lockCtx, cancelLock := context.WithTimeout(ctx, 2*time.Second)
	defer cancelLock()
	lockAudienceRaceProject(t, lockCtx, blocker, principal.OrganizationID, project.ID, cleanup)
	require.NoError(t, blocker.Commit(ctx))
	select {
	case result := <-mutated:
		require.NoError(t, result.err)
		require.False(t, result.output.Receipt.Replayed)
		replay, err := service.SetPluginAssignments(ctx, principal, input)
		require.NoError(t, err)
		require.True(t, replay.Receipt.Replayed)
		require.Equal(t, result.output.Receipt.ID, replay.Receipt.ID)
	case <-ctx.Done():
		t.Fatal("receipt-backed audience mutation did not complete")
	}
}

func TestReconciliationProjectLockStillSerializesProjectUpdate(t *testing.T) {
	t.Parallel()
	testAudienceRaceProjectMutation(t, false)
}

func TestCleanupProjectLockStillSerializesProjectDeletion(t *testing.T) {
	t.Parallel()
	testAudienceRaceProjectMutation(t, true)
}

func testAudienceRaceProjectMutation(t *testing.T, cleanup bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_audience_project_write_lock")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	blocker, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: controlled transaction for deterministic lock interleaving
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return blocker.Rollback(ctx) })
	lockAudienceRaceProject(t, ctx, blocker, principal.OrganizationID, project.ID, cleanup)
	writer, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: independently controlled writer transaction proves lock exclusion
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return writer.Rollback(ctx) })
	written := make(chan error, 1)
	go func() {
		if cleanup {
			_, err := projectrepo.New(writer).DeleteProject(ctx, project.ID)
			written <- err
		} else {
			_, err := projectrepo.New(writer).UpdateProject(ctx, projectrepo.UpdateProjectParams{Name: "Updated after reconciliation", ProjectID: project.ID})
			written <- err
		}
	}()
	waitAudienceRaceProjectWriter(t, ctx, conn, int(blocker.Conn().PgConn().PID()), int(writer.Conn().PgConn().PID()))
	select {
	case err := <-written:
		t.Fatalf("project mutation escaped reconciliation lock: %v", err)
	default:
	}
	require.NoError(t, blocker.Commit(ctx))
	select {
	case err := <-written:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("project mutation did not resume after reconciliation released its lock")
	}
	require.NoError(t, writer.Commit(ctx))
}

func lockAudienceRaceProject(t *testing.T, ctx context.Context, tx pgx.Tx, org string, project uuid.UUID, cleanup bool) {
	t.Helper()
	if cleanup {
		_, err := provisioningrepo.New(tx).LockCleanupProject(ctx, provisioningrepo.LockCleanupProjectParams{OrganizationID: org, ProjectID: project})
		require.NoError(t, err, "cleanup project lock must not conflict with a receipt FK KEY SHARE lock")
	} else {
		_, err := provisioningrepo.New(tx).LockProject(ctx, provisioningrepo.LockProjectParams{OrganizationID: org, ProjectID: project})
		require.NoError(t, err, "reconciliation project lock must not conflict with a receipt FK KEY SHARE lock")
	}
}

func waitAudienceRaceProjectWriter(t *testing.T, ctx context.Context, conn *pgxpool.Pool, blockerPID, writerPID int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRow(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, blockerPID, writerPID).Scan(&waiting) //nolint:glint // notestingrawsql: observe the exact writer's row-lock wait, without timing-based sleeps
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
}
