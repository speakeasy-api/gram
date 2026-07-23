package analysis

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: false, Redis: false})
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

type analysisFixture struct {
	db             *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
}

func newAnalysisFixture(t *testing.T, name string) analysisFixture {
	t.Helper()
	ctx := t.Context()

	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	organizationID := name + "-" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           name,
		Slug:           "analysis-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return analysisFixture{
		db:             db,
		organizationID: organizationID,
		projectID:      project.ID,
	}
}

// enableJudge switches a judge on for the fixture organization with the given
// daily cap.
func (f analysisFixture) enableJudge(t *testing.T, judge string, dailyCap int32) {
	t.Helper()

	_, err := repo.New(f.db).UpsertChatAnalysisSettingsForJudge(t.Context(), repo.UpsertChatAnalysisSettingsForJudgeParams{
		ProjectID: f.projectID,
		Judge:     judge,
		Enabled:   true,
		DailyCap:  dailyCap,
	})
	require.NoError(t, err)
}

// seedChat creates one chat with the given number of messages, the newest
// written lastMessageAge ago. Passing zero messages leaves the chat empty.
func (f analysisFixture) seedChat(t *testing.T, messages int, lastMessageAge time.Duration) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	chatID := uuid.New()
	createdAt := conv.ToPGTimestamptz(time.Now().UTC().Add(-24 * time.Hour))
	queries := chatrepo.New(f.db)
	_, err := queries.UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             chatID,
		ProjectID:      f.projectID,
		OrganizationID: f.organizationID,
		UserID:         conv.ToPGText("user"),
		ExternalUserID: conv.ToPGText("user@example.test"),
		ExternalChatID: pgtype.Text{String: "", Valid: false},
		Title:          conv.ToPGText("chat"),
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	})
	require.NoError(t, err)

	for i := range messages {
		f.seedMessage(t, chatID, lastMessageAge+time.Duration(messages-1-i)*time.Minute)
	}

	return chatID
}

// seedMessage writes one message to an existing chat, age old.
func (f analysisFixture) seedMessage(t *testing.T, chatID uuid.UUID, age time.Duration) {
	t.Helper()

	_, err := chatrepo.New(f.db).SeedChatMessage(t.Context(), chatrepo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		CreatedAt: conv.ToPGTimestamptz(time.Now().UTC().Add(-age)),
	})
	require.NoError(t, err)
}

// pendingEvaluations reads the whole pending queue by walking the keyset pages
// to exhaustion.
func (f analysisFixture) pendingEvaluations(t *testing.T) []repo.ChatAnalysisEvaluation {
	t.Helper()

	queries := repo.New(f.db)
	page := repo.ListPendingChatAnalysisEvaluationsParams{
		ProjectID:        f.projectID,
		CursorObservedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageSize:         PendingCandidatePage,
		Inactivity:       pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
	}

	var all []repo.ChatAnalysisEvaluation
	for {
		rows, err := queries.ListPendingChatAnalysisEvaluations(t.Context(), page)
		require.NoError(t, err)
		if len(rows) == 0 {
			return all
		}
		all = append(all, rows...)
		last := rows[len(rows)-1]
		page.CursorObservedAt = pgtype.Timestamptz{Time: last.ObservedAt.Time, InfinityModifier: pgtype.Finite, Valid: true}
		page.CursorID = uuid.NullUUID{UUID: last.ID, Valid: true}
	}
}

// evaluation loads one row for state assertions.
func (f analysisFixture) evaluation(t *testing.T, id uuid.UUID) repo.ChatAnalysisEvaluation {
	t.Helper()

	row, err := repo.New(f.db).GetChatAnalysisEvaluation(t.Context(), repo.GetChatAnalysisEvaluationParams{
		ProjectID: f.projectID,
		ID:        id,
	})
	require.NoError(t, err)
	return row
}

// stubNamedJudge is a Judge with a name and canned behaviour, for registry and
// pipeline tests.
type stubNamedJudge struct {
	name    string
	verdict JudgeResult
	err     error
}

var _ Judge = stubNamedJudge{}

func (s stubNamedJudge) Name() string { return s.name }

func (s stubNamedJudge) Judge(ctx context.Context, in JudgeInput) (JudgeResult, error) {
	if s.err != nil {
		return JudgeResult{}, s.err
	}
	return s.verdict, nil
}

func stubVerdict(score float64) JudgeResult {
	return JudgeResult{
		Verdict:       Verdict{Score: score, Detail: json.RawMessage(`{"stub":true}`)},
		Model:         "stub-model",
		PromptVersion: "test",
	}
}

// captureSink is an in-memory ScoreSink.
type captureSink struct {
	mu       sync.Mutex
	inserted []telemetryrepo.ChatAnalysisScore
	existing []string
}

var _ ScoreSink = (*captureSink)(nil)

func (c *captureSink) InsertChatAnalysisScores(ctx context.Context, rows []telemetryrepo.ChatAnalysisScore) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inserted = append(c.inserted, rows...)
	return nil
}

func (c *captureSink) ListExistingChatAnalysisScoreIDs(ctx context.Context, arg telemetryrepo.ListExistingChatAnalysisScoreIDsParams) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.existing))
	copy(out, c.existing)
	return out, nil
}

func (c *captureSink) rows(t *testing.T) []telemetryrepo.ChatAnalysisScore {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]telemetryrepo.ChatAnalysisScore, len(c.inserted))
	copy(out, c.inserted)
	return out
}
