package activities_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type failingSkillVersionConn struct {
	mock.Mock
}

func (f *failingSkillVersionConn) Exec(ctx context.Context, query string, args ...any) error {
	callArgs := f.Called(ctx, query, args)
	return callArgs.Error(0)
}

func (f *failingSkillVersionConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	return nil, nil
}

func TestSkillObservationReconcilerSyncSessionVersionsInsertsBeforeMarkAndRetriesSafely(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "skill_observation_sync")
	require.NoError(t, err)

	organizationID := "skill-observation-sync-" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "skill-observation-sync",
		Slug:           "skill-observation-sync-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	content := "---\nname: sync-skill\ndescription: Sync skill.\n---\n\nBody\n"
	version, err := skills.CaptureSkillContent(ctx, db, project.ID, content)
	require.NoError(t, err)
	rawHash := sha256.Sum256([]byte(content))
	require.NoError(t, hooksrepo.New(db).InsertSkillObservation(ctx, hooksrepo.InsertSkillObservationParams{
		ProjectID:      project.ID,
		IdempotencyKey: conv.ToPGText(uuid.NewString()),
		Provider:       "assistants",
		UserID:         pgtype.Text{},
		UserEmail:      pgtype.Text{},
		Hostname:       pgtype.Text{},
		SessionID:      conv.ToPGText("session-sync"),
		SkillName:      "sync-skill",
		Source:         pgtype.Text{},
		SourceLevel:    conv.ToPGText("project"),
		SourcePath:     pgtype.Text{},
		RawSha256:      conv.ToPGText(hex.EncodeToString(rawHash[:])),
		SeenAt:         conv.ToPGTimestamptz(time.Now().UTC()),
	}))
	result, err := skills.ReconcileSkillObservations(ctx, db, project.ID, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)

	failingConn := new(failingSkillVersionConn)
	failingConn.On("Exec", mock.Anything, mock.Anything, mock.Anything).Return(context.DeadlineExceeded).Once()
	reconciler := activities.NewSkillObservationReconciler(db, telemetryrepo.New(failingConn))
	_, err = reconciler.SyncSessionVersions(ctx, activities.SyncSkillSessionVersionsParams{ProjectID: project.ID, BatchSize: 10})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	failingConn.AssertExpectations(t)

	pending, err := skillsrepo.New(db).ListPendingSkillSessionVersions(ctx, skillsrepo.ListPendingSkillSessionVersionsParams{
		ProjectID: project.ID,
		BatchSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, pending, 1, "a failed insert must not mark the observation synced")

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	reconciler = activities.NewSkillObservationReconciler(db, telemetryrepo.New(chConn))
	synced, err := reconciler.SyncSessionVersions(ctx, activities.SyncSkillSessionVersionsParams{ProjectID: project.ID, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, synced.Processed)

	synced, err = reconciler.SyncSessionVersions(ctx, activities.SyncSkillSessionVersionsParams{ProjectID: project.ID, BatchSize: 10})
	require.NoError(t, err)
	require.Zero(t, synced.Processed)

	testenv.FlushClickHouseAsyncInserts(t, chConn)
	var count uint64
	err = chConn.QueryRow(ctx, "SELECT count() FROM skill_session_versions WHERE id = toUUID(?)", pending[0].ID.String()).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
	require.Equal(t, version.SkillVersionID, pending[0].SkillVersionID)
	require.Equal(t, "assistant", pending[0].Surface)
}
