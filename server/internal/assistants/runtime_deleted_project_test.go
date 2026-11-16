package assistants

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Unlike insertReapableRuntimeRow, these fixtures deliberately leave ended_at
// and deleted_at unset, even for stopped rows, unless finalized is requested.
func insertDeletedProjectRuntime(t *testing.T, conn *pgxpool.Pool, projectID, assistantID, threadID uuid.UUID, state string, updatedAt time.Time, finalized ...bool) uuid.UUID {
	t.Helper()
	var endedAt pgtype.Timestamptz
	if len(finalized) > 0 && finalized[0] {
		endedAt = pgtype.Timestamptz{Time: updatedAt, Valid: true}
	}
	id := uuid.New()
	require.NoError(t, assistantsrepo.New(conn).CreateAssistantRuntime(t.Context(), assistantsrepo.CreateAssistantRuntimeParams{
		ID: id, ProjectID: projectID, AssistantID: assistantID, AssistantThreadID: threadID,
		Backend: runtimeBackendLocal, BackendMetadataJson: []byte(`{"container_name":"test-runtime"}`),
		State: state, UpdatedAt: pgtype.Timestamptz{Time: updatedAt, Valid: true}, EndedAt: endedAt,
	}))
	return id
}

func deletedProjectReaperCore(t *testing.T, conn *pgxpool.Pool, calls *atomic.Int64, reapErr error) *ServiceCore {
	t.Helper()
	backend := testRuntimeBackend{backend: runtimeBackendLocal, reapCalls: calls, reapErr: reapErr}
	return NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))
}

func TestServiceCoreReapInactiveDeletedProjects(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_deleted_projects")
	require.NoError(t, err)

	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	type fixture struct {
		projectID   uuid.UUID
		assistantID uuid.UUID
		runtimeID   uuid.UUID
		state       string
		collect     bool
	}
	var fixtures []fixture
	for _, tc := range []struct {
		name    string
		state   string
		deleted bool
		collect bool
	}{
		{"deleted-active", runtimeStateActive, true, true},
		{"deleted-stopped", runtimeStateStopped, true, true},
		{"live-active", runtimeStateActive, false, false},
		{"live-starting", runtimeStateStarting, false, false},
		{"live-stopped", runtimeStateStopped, false, false},
	} {
		projectID, assistantID, threadID := insertReapableProject(t, conn, tc.name)
		runtimeID := insertDeletedProjectRuntime(t, conn, projectID, assistantID, threadID, tc.state, stale)
		if tc.deleted {
			_, err := projectsrepo.New(conn).DeleteProject(t.Context(), projectID)
			require.NoError(t, err)
		}
		fixtures = append(fixtures, fixture{projectID, assistantID, runtimeID, tc.state, tc.collect})
	}

	calls := &atomic.Int64{}
	result, err := deletedProjectReaperCore(t, conn, calls, nil).ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour, BatchSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Reaped)
	require.Zero(t, result.Errors)
	require.EqualValues(t, 2, calls.Load())
	for _, f := range fixtures {
		assistant, err := assistantsrepo.New(conn).GetAssistantIgnoringDeleted(t.Context(), assistantsrepo.GetAssistantIgnoringDeletedParams{ProjectID: f.projectID, AssistantID: f.assistantID})
		require.NoError(t, err)
		require.False(t, assistant.DeletedAt.Valid)
		row, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ProjectID: f.projectID, ID: f.runtimeID})
		require.NoError(t, err)
		if f.collect {
			require.Equal(t, runtimeStateReaped, row.State)
			require.JSONEq(t, `{}`, string(row.BackendMetadataJson))
		} else {
			require.Equal(t, f.state, row.State)
			require.JSONEq(t, `{"container_name":"test-runtime"}`, string(row.BackendMetadataJson))
			require.False(t, row.Ended)
			require.False(t, row.Deleted)
		}
	}
}

func TestServiceCoreReapInactiveDeletedProjectsPreservesRecentActivity(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_deleted_projects_recent")
	require.NoError(t, err)
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-time.Hour)
	for _, sibling := range []bool{false, true} {
		name := "recent-runtime"
		if sibling {
			name = "recent-sibling"
		}
		projectID, assistantID, threadID := insertReapableProject(t, conn, name)
		if sibling {
			insertDeletedProjectRuntime(t, conn, projectID, assistantID, threadID, runtimeStateStopped, stale, true)
		}
		insertDeletedProjectRuntime(t, conn, projectID, assistantID, threadID, runtimeStateActive, recent)
		_, err := projectsrepo.New(conn).DeleteProject(t.Context(), projectID)
		require.NoError(t, err)
	}
	calls := &atomic.Int64{}
	result, err := deletedProjectReaperCore(t, conn, calls, nil).ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: 7 * 24 * time.Hour, BatchSize: 10,
	})
	require.NoError(t, err)
	require.Zero(t, result.Reaped)
	require.Zero(t, result.Errors)
	require.Zero(t, calls.Load())
}

func TestServiceCoreReapInactiveDeletedProjectsBatchAndRetry(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "reap_deleted_projects_retry")
	require.NoError(t, err)
	projectID, assistantID, threadID := insertReapableProject(t, conn, "deleted-retry")
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	ids := []uuid.UUID{
		insertDeletedProjectRuntime(t, conn, projectID, assistantID, threadID, runtimeStateStopped, stale, true),
		insertDeletedProjectRuntime(t, conn, projectID, assistantID, threadID, runtimeStateActive, stale.Add(time.Hour)),
	}
	_, err = projectsrepo.New(conn).DeleteProject(t.Context(), projectID)
	require.NoError(t, err)
	params := ReapInactiveAssistantRuntimesParams{InactivityThreshold: 7 * 24 * time.Hour, BatchSize: 1}
	calls := &atomic.Int64{}
	result, err := deletedProjectReaperCore(t, conn, calls, errors.New("temporary reap failure")).ReapInactiveAssistantRuntimes(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, result.Reaped)
	require.Equal(t, 1, result.Errors)
	require.EqualValues(t, 1, calls.Load())
	for _, id := range ids {
		row, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ProjectID: projectID, ID: id})
		require.NoError(t, err)
		require.NotEqual(t, runtimeStateReaped, row.State)
		require.JSONEq(t, `{"container_name":"test-runtime"}`, string(row.BackendMetadataJson))
	}
	core := deletedProjectReaperCore(t, conn, calls, nil)
	for _, id := range ids {
		result, err := core.ReapInactiveAssistantRuntimes(t.Context(), params)
		require.NoError(t, err)
		require.Equal(t, 1, result.Reaped)
		require.Zero(t, result.Errors)
		row, err := assistantsrepo.New(conn).GetAssistantRuntime(t.Context(), assistantsrepo.GetAssistantRuntimeParams{ProjectID: projectID, ID: id})
		require.NoError(t, err)
		require.Equal(t, runtimeStateReaped, row.State)
		require.JSONEq(t, `{}`, string(row.BackendMetadataJson))
	}
	require.EqualValues(t, 3, calls.Load())
}

func TestListActiveAssistantRuntimesExcludesDeletedProjects(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "active_deleted_projects")
	require.NoError(t, err)
	queries := assistantsrepo.New(conn)
	var liveID uuid.UUID
	for _, deleted := range []bool{false, true} {
		name := "active-live-project"
		if deleted {
			name = "active-deleted-project"
		}
		projectID, assistantID, threadID := insertReapableProject(t, conn, name)
		runtimeID := insertDeletedProjectRuntime(t, conn, projectID, assistantID, threadID, runtimeStateActive, time.Now().UTC().Add(-30*24*time.Hour))
		require.NoError(t, queries.SetKitRuntimeMetadataFixture(t.Context(), assistantsrepo.SetKitRuntimeMetadataFixtureParams{
			ProjectID: projectID, RuntimeID: runtimeID, RuntimeVersion: 2,
			Metadata: []byte(`{"container_name":"test-runtime"}`),
		}))
		if deleted {
			_, err := projectsrepo.New(conn).DeleteProject(t.Context(), projectID)
			require.NoError(t, err)
		} else {
			liveID = runtimeID
		}
	}
	rows, err := queries.ListActiveAssistantRuntimes(t.Context(), runtimeStateActive)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, liveID, rows[0].ID)
}
