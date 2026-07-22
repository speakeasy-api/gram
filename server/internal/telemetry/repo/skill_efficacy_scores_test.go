package repo_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: false, Redis: false, ClickHouse: true, Temporal: false, Presidio: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// efficacyScoreFixture builds a score row that satisfies every CHECK constraint
// on skill_efficacy_scores, so a test only has to override what it exercises.
func efficacyScoreFixture(t *testing.T, orgID string, projectID string, createdAt time.Time) repo.SkillEfficacyScore {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)
	skillID, err := uuid.NewV7()
	require.NoError(t, err)
	skillVersionID, err := uuid.NewV7()
	require.NoError(t, err)

	confidence := "high"

	return repo.SkillEfficacyScore{
		ID:                 id,
		CreatedAt:          createdAt,
		OrganizationID:     orgID,
		ProjectID:          projectID,
		SessionID:          "session-" + id.String(),
		SkillID:            skillID,
		SkillVersionID:     skillVersionID,
		CanonicalSHA256:    "0000000000000000000000000000000000000000000000000000000000000000",
		Surface:            "dev",
		TraceID:            nil,
		GramChatID:         "",
		Score:              0.75,
		Rationale:          "the skill was followed and shortened the session",
		EstTurnsSaved:      nil,
		EstMinutesSaved:    nil,
		ROIConfidence:      &confidence,
		Flags:              []string{"partially_followed"},
		JudgeModel:         "test-judge-model",
		JudgePromptVersion: "v1",
	}
}

// guardParams builds the filters the publication dedup guard sends for one row,
// so a test only has to override the field it isolates on.
func guardParams(row repo.SkillEfficacyScore, createdAt time.Time) repo.ListExistingSkillEfficacyScoreIDsParams {
	return repo.ListExistingSkillEfficacyScoreIDsParams{
		OrganizationID:  row.OrganizationID,
		ProjectID:       row.ProjectID,
		SkillIDs:        []string{row.SkillID.String()},
		SkillVersionIDs: []string{row.SkillVersionID.String()},
		IDs:             []string{row.ID.String()},
		MinCreatedAt:    createdAt.Add(-time.Hour),
		MaxCreatedAt:    createdAt.Add(time.Hour),
	}
}

func TestInsertSkillEfficacyScores_SynchronousReadBack(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	turns := 3.0
	minutes := 12.5
	traceID := "0123456789abcdef0123456789abcdef"
	row.EstTurnsSaved = &turns
	row.EstMinutesSaved = &minutes
	row.TraceID = &traceID
	row.GramChatID = uuid.NewString()

	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	// No polling and no async-queue flush: the insert is synchronous, so the
	// guard read publication runs next always sees the row.
	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, guardParams(row, createdAt))
	require.NoError(t, err)
	require.Equal(t, []string{row.ID.String()}, got)
}

func TestInsertSkillEfficacyScores_EmptyInputIsNoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, nil))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{}))
}

func TestListExistingSkillEfficacyScoreIDs_EmptyIDsSkipsQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, repo.ListExistingSkillEfficacyScoreIDsParams{
		OrganizationID:  uuid.NewString(),
		ProjectID:       uuid.NewString(),
		SkillIDs:        []string{uuid.NewString()},
		SkillVersionIDs: []string{uuid.NewString()},
		IDs:             nil,
		MinCreatedAt:    time.Now().UTC().Add(-time.Hour),
		MaxCreatedAt:    time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestListExistingSkillEfficacyScoreIDs_FiltersByRequestedIDs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	present := efficacyScoreFixture(t, orgID, projectID, createdAt)
	other := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{present, other}))

	absent, err := uuid.NewV7()
	require.NoError(t, err)

	params := guardParams(present, createdAt)
	params.SkillIDs = append(params.SkillIDs, other.SkillID.String())
	params.SkillVersionIDs = append(params.SkillVersionIDs, other.SkillVersionID.String())
	params.IDs = append(params.IDs, absent.String())

	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []string{present.ID.String()}, got)
}

func TestListExistingSkillEfficacyScoreIDs_EmptySkillFiltersSkipQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	// An empty key set can match nothing, so it never reaches ClickHouse as an
	// unbounded read.
	withoutSkills := guardParams(row, createdAt)
	withoutSkills.SkillIDs = nil
	noSkills, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, withoutSkills)
	require.NoError(t, err)
	require.Empty(t, noSkills)

	withoutVersions := guardParams(row, createdAt)
	withoutVersions.SkillVersionIDs = nil
	noVersions, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, withoutVersions)
	require.NoError(t, err)
	require.Empty(t, noVersions)
}

func TestListExistingSkillEfficacyScoreIDs_IsolatedBySkill(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	otherSkillID, err := uuid.NewV7()
	require.NoError(t, err)

	params := guardParams(row, createdAt)
	params.SkillIDs = []string{otherSkillID.String()}

	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, params)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestListExistingSkillEfficacyScoreIDs_IsolatedBySkillVersion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	otherVersionID, err := uuid.NewV7()
	require.NoError(t, err)

	params := guardParams(row, createdAt)
	params.SkillVersionIDs = []string{otherVersionID.String()}

	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, params)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestListExistingSkillEfficacyScoreIDs_IsolatedByProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	otherProjectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	params := guardParams(row, createdAt)
	params.ProjectID = otherProjectID

	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, params)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestListExistingSkillEfficacyScoreIDs_IsolatedByOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	otherOrgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	params := guardParams(row, createdAt)
	params.OrganizationID = otherOrgID

	got, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, params)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestListExistingSkillEfficacyScoreIDs_BoundedByCreatedAtWindow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	createdAt := time.Now().UTC()

	row := efficacyScoreFixture(t, orgID, projectID, createdAt)
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row}))

	inside, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, guardParams(row, createdAt))
	require.NoError(t, err)
	require.Equal(t, []string{row.ID.String()}, inside)

	laterWindow := guardParams(row, createdAt)
	laterWindow.MinCreatedAt = createdAt.Add(24 * time.Hour)
	laterWindow.MaxCreatedAt = createdAt.Add(48 * time.Hour)

	outside, err := queries.ListExistingSkillEfficacyScoreIDs(ctx, laterWindow)
	require.NoError(t, err)
	require.Empty(t, outside)
}
