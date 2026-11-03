package summaryjudge_test

import (
	"context"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/summaryjudge"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/memorygate"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
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

// stubClient answers every completion with a fixed summary and records the
// prompt it was sent. Methods the judge must not call panic through the nil
// embedded interface.
type stubClient struct {
	openrouter.CompletionClient
	summary string
	calls   atomic.Int32
	prompt  atomic.Pointer[string]
}

func (s *stubClient) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	s.calls.Add(1)
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(openrouter.GetText(m))
	}
	prompt := b.String()
	s.prompt.Store(&prompt)

	content := or.CreateChatAssistantMessageContentStr(s.summary)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	return &openrouter.CompletionResponse{
		StartTime:    time.Time{},
		Message:      &msg,
		MessageID:    "msg_test",
		Model:        "test-model",
		Usage:        openrouter.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      s.summary,
	}, nil
}

type fixture struct {
	db        *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
}

func newFixture(t *testing.T, name string) fixture {
	t.Helper()
	ctx := t.Context()

	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	orgID := name + "-" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           name,
		Slug:           "summary-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	return fixture{db: db, orgID: orgID, projectID: project.ID}
}

// seedChat creates a chat with one user message per content string.
func (f fixture) seedChat(t *testing.T, contents ...string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	chatID, err := chatrepo.New(f.db).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      f.projectID,
		OrganizationID: f.orgID,
		UserID:         conv.ToPGText("user"),
		ExternalUserID: pgtype.Text{String: "", Valid: false},
		Title:          pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	for _, content := range contents {
		_, err := chatrepo.New(f.db).SeedChatMessageContent(ctx, chatrepo.SeedChatMessageContentParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
			Content:   content,
		})
		require.NoError(t, err)
	}
	return chatID
}

// reserve enqueues and reserves the chat's summary evaluation directly,
// bypassing the quiet window, and returns its id.
func (f fixture) reserve(t *testing.T, chatID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	queries := analysisrepo.New(f.db)
	err := queries.EnqueueChatAnalysisEvaluations(ctx, analysisrepo.EnqueueChatAnalysisEvaluationsParams{
		ProjectID:   f.projectID,
		ChatIds:     []uuid.UUID{chatID},
		SessionIds:  []string{""},
		ObservedAts: []pgtype.Timestamptz{conv.ToPGTimestamptz(time.Now().Add(-time.Hour))},
		Judges:      []string{summaryjudge.JudgeName},
		RearmJudges: []string{summaryjudge.JudgeName},
	})
	require.NoError(t, err)
	ids, err := queries.ReserveChatAnalysisEvaluations(ctx, analysisrepo.ReserveChatAnalysisEvaluationsParams{
		ReservedOn: pgtype.Date{Time: time.Now().UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		ProjectID:  f.projectID,
		Ids:        f.evaluationIDs(t),
		Inactivity: pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	require.Len(t, ids, 1)
	return ids[0]
}

func (f fixture) evaluationIDs(t *testing.T) []uuid.UUID {
	t.Helper()
	rows, err := analysisrepo.New(f.db).ListPendingChatAnalysisEvaluations(t.Context(), analysisrepo.ListPendingChatAnalysisEvaluationsParams{
		ProjectID:        f.projectID,
		CursorObservedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageSize:         analysis.PendingCandidatePage,
		Inactivity:       pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func (f fixture) judge(t *testing.T, client openrouter.CompletionClient, memoryOn bool) *summaryjudge.Judge {
	t.Helper()
	require.NoError(t, memorygate.Set(t.Context(), f.db, f.orgID, memoryOn))
	return summaryjudge.NewJudge(testenv.NewLogger(t), f.db, client, nil)
}

func (f fixture) input(chatID, evaluationID uuid.UUID) analysis.JudgeInput {
	return analysis.JudgeInput{
		EvaluationID: evaluationID,
		OrgID:        f.orgID,
		ProjectID:    f.projectID.String(),
		ChatID:       chatID,
		AuthorID:     "",
	}
}

func (f fixture) chat(t *testing.T, chatID uuid.UUID) chatrepo.GetChatRow {
	t.Helper()
	row, err := chatrepo.New(f.db).GetChat(t.Context(), chatrepo.GetChatParams{ID: chatID, ProjectID: f.projectID})
	require.NoError(t, err)
	return row
}

func (f fixture) state(t *testing.T, evaluationID uuid.UUID) string {
	t.Helper()
	row, err := analysisrepo.New(f.db).GetChatAnalysisEvaluation(t.Context(), analysisrepo.GetChatAnalysisEvaluationParams{ProjectID: f.projectID, ID: evaluationID})
	require.NoError(t, err)
	return row.State
}

func TestJudge_WritesMaskedSummaryAndScoresEvaluation(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "summaryjudge_writes")
	key := "sk-proj-" + strings.Repeat("4f9Kq2Lm", 4)
	chatID := f.seedChat(t, "We moved retries onto the durable queue; key "+key)
	evaluationID := f.reserve(t, chatID)

	client := &stubClient{summary: "The team moved retries to the durable queue using " + key + "."}
	result, err := f.judge(t, client, true).Judge(t.Context(), f.input(chatID, evaluationID))
	require.NoError(t, err)
	require.InDelta(t, 1.0, result.Verdict.Score, 0)
	require.JSONEq(t, `{"outcome":"written"}`, string(result.Verdict.Detail))

	require.Equal(t, int32(1), client.calls.Load())
	require.NotContains(t, *client.prompt.Load(), key)

	stored := f.chat(t, chatID)
	require.True(t, stored.SummaryRedacted)
	require.NotContains(t, stored.Summary.String, key)
	require.Contains(t, stored.Summary.String, "durable queue")
	require.Equal(t, "scored", f.state(t, evaluationID))
}

func TestJudge_MemoryOffScoresWithoutModelCall(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "summaryjudge_memoryoff")
	chatID := f.seedChat(t, "Decided to keep the old auth flow")
	evaluationID := f.reserve(t, chatID)

	client := &stubClient{summary: "unused"}
	result, err := f.judge(t, client, false).Judge(t.Context(), f.input(chatID, evaluationID))
	require.NoError(t, err)
	require.JSONEq(t, `{"outcome":"memory_off"}`, string(result.Verdict.Detail))
	require.Zero(t, client.calls.Load())
	require.False(t, f.chat(t, chatID).Summary.Valid)
	require.Equal(t, "scored", f.state(t, evaluationID))
}

func TestJudge_SkipsChatsExcludedFromMemory(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "summaryjudge_excluded")
	chatID := f.seedChat(t, "This one opted out of memory")
	evaluationID := f.reserve(t, chatID)
	err := testrepo.New(f.db).SetChatMemorySweepFixture(t.Context(), testrepo.SetChatMemorySweepFixtureParams{
		UpdatedAt:          conv.ToPGTimestamptz(time.Now()),
		SummaryGeneratedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		MemoryExcludedAt:   conv.ToPGTimestamptz(time.Now()),
		ID:                 chatID,
	})
	require.NoError(t, err)

	client := &stubClient{summary: "unused"}
	result, err := f.judge(t, client, true).Judge(t.Context(), f.input(chatID, evaluationID))
	require.NoError(t, err)
	require.JSONEq(t, `{"outcome":"skipped"}`, string(result.Verdict.Detail))
	require.Zero(t, client.calls.Load())
	require.Equal(t, "scored", f.state(t, evaluationID))
}
