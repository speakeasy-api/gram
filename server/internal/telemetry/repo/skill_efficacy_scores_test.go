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

func TestQuerySkillInsightsAggregatesMappingsAndScoresWithoutUsage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.New()
	observedAt := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	score := efficacyScoreFixture(t, orgID, projectID.String(), observedAt.Add(40*time.Minute))
	score.SessionID = "insight-session"
	score.Score = 0.8
	turns := 2.0
	minutes := 8.0
	score.EstTurnsSaved = &turns
	score.EstMinutesSaved = &minutes
	score.Flags = []string{"partially_followed", "ignored"}

	mappings := []repo.SkillSessionVersion{
		{
			ID:              uuid.New(),
			CreatedAt:       observedAt,
			SeenAt:          observedAt,
			OrganizationID:  orgID,
			ProjectID:       projectID,
			SessionID:       score.SessionID,
			SkillID:         score.SkillID,
			SkillVersionID:  score.SkillVersionID,
			CanonicalSHA256: score.CanonicalSHA256,
			Surface:         score.Surface,
		},
		{
			ID:              uuid.New(),
			CreatedAt:       observedAt.Add(time.Minute),
			SeenAt:          observedAt.Add(time.Minute),
			OrganizationID:  orgID,
			ProjectID:       projectID,
			SessionID:       score.SessionID,
			SkillID:         score.SkillID,
			SkillVersionID:  score.SkillVersionID,
			CanonicalSHA256: score.CanonicalSHA256,
			Surface:         score.Surface,
		},
	}
	require.NoError(t, queries.InsertSkillSessionVersions(ctx, mappings))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{score}))
	testenv.FlushClickHouseAsyncInserts(t, conn)

	rows, err := queries.QuerySkillInsights(ctx, repo.QuerySkillInsightsParams{
		OrganizationID:  orgID,
		ProjectID:       projectID.String(),
		SkillIDs:        []string{score.SkillID.String()},
		SkillVersionIDs: nil,
		From:            observedAt.Add(-time.Hour),
		To:              observedAt.Add(time.Hour),
		IntervalSeconds: int64((24 * time.Hour).Seconds()),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, score.SkillID.String(), rows[0].SkillID)
	require.Equal(t, score.SkillVersionID.String(), rows[0].SkillVersionID)
	require.EqualValues(t, 2, rows[0].ActivationCount)
	require.EqualValues(t, 1, rows[0].ActivatedSessions)
	require.Zero(t, rows[0].TotalSessionCost)
	require.EqualValues(t, 1, rows[0].ScoredSessions)
	require.InDelta(t, 0.8, rows[0].ScoreSum, 0)
	require.InDelta(t, 2, rows[0].EstimatedTurnsSavedSum, 0)
	require.EqualValues(t, 1, rows[0].EstimatedTurnsSamples)
	require.InDelta(t, 8, rows[0].EstimatedMinutesSavedSum, 0)
	require.EqualValues(t, 1, rows[0].EstimatedMinutesSamples)
	require.EqualValues(t, 1, rows[0].ROIConfidenceHigh)
	require.EqualValues(t, 1, rows[0].IgnoredCount)
	require.EqualValues(t, 1, rows[0].PartiallyFollowedCount)
}

func TestQuerySkillInsightsDeduplicatesPhysicalScoresByEventID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.New()
	observedAt := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	score := efficacyScoreFixture(t, orgID, projectID.String(), observedAt.Add(30*time.Minute))
	score.SessionID = "deduplicated-insight-session"
	score.Score = 0.8
	minutes := 8.0
	score.EstMinutesSaved = &minutes
	score.Flags = []string{"ignored"}
	duplicate := score
	duplicate.CreatedAt = score.CreatedAt.Add(time.Minute)
	duplicate.Score = 0.1
	duplicate.Rationale = "duplicate retry"
	duplicateMinutes := 80.0
	duplicate.EstMinutesSaved = &duplicateMinutes
	duplicate.Flags = []string{"harmful"}

	require.NoError(t, queries.InsertSkillSessionVersions(ctx, []repo.SkillSessionVersion{{
		ID: uuid.New(), CreatedAt: observedAt, SeenAt: observedAt, OrganizationID: orgID, ProjectID: projectID,
		SessionID: score.SessionID, SkillID: score.SkillID, SkillVersionID: score.SkillVersionID,
		CanonicalSHA256: score.CanonicalSHA256, Surface: score.Surface,
	}}))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{score}))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{duplicate}))
	testenv.FlushClickHouseAsyncInserts(t, conn)

	rows, err := queries.QuerySkillInsights(ctx, repo.QuerySkillInsightsParams{
		OrganizationID: orgID, ProjectID: projectID.String(), SkillIDs: []string{score.SkillID.String()}, SkillVersionIDs: nil,
		From: observedAt.Add(-time.Hour), To: observedAt.Add(time.Hour), IntervalSeconds: int64((24 * time.Hour).Seconds()),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].ScoredSessions)
	require.InDelta(t, 0.8, rows[0].ScoreSum, 0)
	require.InDelta(t, 8, rows[0].EstimatedMinutesSavedSum, 0)
	require.EqualValues(t, 1, rows[0].EstimatedMinutesSamples)
	require.EqualValues(t, 1, rows[0].IgnoredCount)
	require.Zero(t, rows[0].HarmfulCount)
}

func TestListSkillEfficacyScoreSessionsReturnsActivationAndVerdict(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.New()
	activatedAt := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	score := efficacyScoreFixture(t, orgID, projectID.String(), activatedAt.Add(40*time.Minute))
	score.SessionID = "scored-session-list"
	score.Surface = "assistant"
	score.GramChatID = uuid.NewString()
	minutes := 7.0
	score.EstMinutesSaved = &minutes
	require.NoError(t, queries.InsertSkillSessionVersions(ctx, []repo.SkillSessionVersion{{
		ID:              uuid.New(),
		CreatedAt:       activatedAt,
		SeenAt:          activatedAt,
		OrganizationID:  orgID,
		ProjectID:       projectID,
		SessionID:       score.SessionID,
		SkillID:         score.SkillID,
		SkillVersionID:  score.SkillVersionID,
		CanonicalSHA256: score.CanonicalSHA256,
		Surface:         score.Surface,
	}}))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{score}))
	testenv.FlushClickHouseAsyncInserts(t, conn)

	rows, err := queries.ListSkillEfficacyScoreSessions(ctx, repo.ListSkillEfficacyScoreSessionsParams{
		OrganizationID: orgID,
		ProjectID:      projectID.String(),
		SkillIDs:       []string{score.SkillID.String()},
		From:           activatedAt.Add(-time.Hour),
		To:             activatedAt.Add(time.Hour),
		Limit:          100,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, score.ID.String(), rows[0].ID)
	require.Equal(t, activatedAt, rows[0].ActivatedAt)
	require.Equal(t, score.Rationale, rows[0].Rationale)
	require.Equal(t, score.GramChatID, rows[0].GramChatID)
	require.Equal(t, score.Flags, rows[0].Flags)
}

func TestListSkillEfficacyScoreSessionsDeduplicatesBeforeLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := uuid.NewString()
	projectID := uuid.New()
	activatedAt := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	first := efficacyScoreFixture(t, orgID, projectID.String(), activatedAt.Add(40*time.Minute))
	first.SessionID = "deduplicated-session-first"
	first.Rationale = "first publication"
	duplicate := first
	duplicate.CreatedAt = first.CreatedAt.Add(time.Minute)
	duplicate.Rationale = "duplicate retry"
	second := efficacyScoreFixture(t, orgID, projectID.String(), first.CreatedAt.Add(-time.Minute))
	second.SessionID = "deduplicated-session-second"
	second.SkillID = first.SkillID
	second.SkillVersionID = first.SkillVersionID
	second.CanonicalSHA256 = first.CanonicalSHA256
	second.Surface = first.Surface

	mappings := []repo.SkillSessionVersion{
		{ID: uuid.New(), CreatedAt: activatedAt, SeenAt: activatedAt, OrganizationID: orgID, ProjectID: projectID, SessionID: first.SessionID, SkillID: first.SkillID, SkillVersionID: first.SkillVersionID, CanonicalSHA256: first.CanonicalSHA256, Surface: first.Surface},
		{ID: uuid.New(), CreatedAt: activatedAt.Add(time.Minute), SeenAt: activatedAt.Add(time.Minute), OrganizationID: orgID, ProjectID: projectID, SessionID: second.SessionID, SkillID: second.SkillID, SkillVersionID: second.SkillVersionID, CanonicalSHA256: second.CanonicalSHA256, Surface: second.Surface},
	}
	require.NoError(t, queries.InsertSkillSessionVersions(ctx, mappings))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{first, second}))
	require.NoError(t, queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{duplicate}))
	testenv.FlushClickHouseAsyncInserts(t, conn)

	rows, err := queries.ListSkillEfficacyScoreSessions(ctx, repo.ListSkillEfficacyScoreSessionsParams{
		OrganizationID: orgID, ProjectID: projectID.String(), SkillIDs: []string{first.SkillID.String()},
		From: activatedAt.Add(-time.Hour), To: activatedAt.Add(time.Hour), Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, first.ID.String(), rows[0].ID)
	require.Equal(t, "first publication", rows[0].Rationale)
	require.Equal(t, second.ID.String(), rows[1].ID)
}

func TestInsertSkillEfficacyScores_ClassifiesConstraintViolations(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	row := efficacyScoreFixture(t, uuid.NewString(), uuid.NewString(), time.Now().UTC())
	row.Surface = "invalid"

	err = queries.InsertSkillEfficacyScores(ctx, []repo.SkillEfficacyScore{row})
	require.ErrorIs(t, err, repo.ErrInvalidSkillEfficacyScore)
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
