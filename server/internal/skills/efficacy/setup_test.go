package efficacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: false, Redis: true})
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

const skillContent = "---\nname: efficacy-skill\ndescription: Efficacy skill.\n---\n\nBody\n"

type efficacyFixture struct {
	db             *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	skillVersionID uuid.UUID
}

func newEfficacyFixture(t *testing.T, name string) efficacyFixture {
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
		Slug:           "efficacy-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	captured, err := skills.CaptureSkillContent(ctx, db, project.ID, skillContent)
	require.NoError(t, err)

	return efficacyFixture{
		db:             db,
		organizationID: organizationID,
		projectID:      project.ID,
		skillVersionID: captured.SkillVersionID,
	}
}

// enableJudge switches the skill efficacy judge on for the fixture
// organization with the given daily cap.
func (f efficacyFixture) enableJudge(t *testing.T, dailyCap int32) {
	t.Helper()

	_, err := analysisrepo.New(f.db).UpsertChatAnalysisSettingForOrganizationJudge(t.Context(), analysisrepo.UpsertChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: f.organizationID,
		Judge:          JudgeName,
		Enabled:        true,
		DailyCap:       dailyCap,
	})
	require.NoError(t, err)
}

// seedChat creates the chat a session's activations belong to exactly the way
// the hook capture paths do: the chat id is the session id mapped through
// chat.SessionIDToChatID. lastMessageAge is how long ago the newest message was
// written.
func (f efficacyFixture) seedChat(t *testing.T, sessionID string, messages int, lastMessageAge time.Duration) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	chatID := chat.SessionIDToChatID(sessionID)
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
		age := lastMessageAge + time.Duration(messages-1-i)*time.Minute
		_, err := queries.SeedChatMessage(ctx, chatrepo.SeedChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
			CreatedAt: conv.ToPGTimestamptz(time.Now().UTC().Add(-age)),
		})
		require.NoError(t, err)
	}

	return chatID
}

// deleteChat soft-deletes a chat directly, the state a deleted-session unit
// sees.
func (f efficacyFixture) deleteChat(t *testing.T, chatID uuid.UUID) {
	t.Helper()

	row, err := chatrepo.New(f.db).SoftDeleteChat(t.Context(), chatrepo.SoftDeleteChatParams{
		ID:        chatID,
		ProjectID: f.projectID,
	})
	require.NoError(t, err)
	require.True(t, row.Deleted)
}

// observe records one activation of the fixture skill and reconciles it, which
// is what makes it a unit-source candidate.
func (f efficacyFixture) observe(t *testing.T, sessionID string, provider string, seenAt time.Time) {
	t.Helper()
	ctx := t.Context()

	rawHash := sha256.Sum256([]byte(skillContent))
	_, err := hooksrepo.New(f.db).InsertSkillObservation(ctx, hooksrepo.InsertSkillObservationParams{
		ProjectID:      f.projectID,
		IdempotencyKey: conv.ToPGText(uuid.NewString()),
		Provider:       provider,
		UserID:         conv.ToPGText("user"),
		UserEmail:      conv.ToPGText("user@example.test"),
		Hostname:       pgtype.Text{String: "", Valid: false},
		SessionID:      conv.ToPGText(sessionID),
		SkillName:      "efficacy-skill",
		Source:         pgtype.Text{String: "", Valid: false},
		SourceLevel:    conv.ToPGText("project"),
		SourcePath:     pgtype.Text{String: "", Valid: false},
		RawSha256:      conv.ToPGText(hex.EncodeToString(rawHash[:])),
		SeenAt:         conv.ToPGTimestamptz(seenAt),
	})
	require.NoError(t, err)

	_, err = skills.ReconcileSkillObservations(ctx, f.db, f.projectID, 50)
	require.NoError(t, err)
}

// pendingUnits reads the project's whole pending analysis queue.
func (f efficacyFixture) pendingUnits(t *testing.T) []analysisrepo.ChatAnalysisEvaluation {
	t.Helper()

	rows, err := analysisrepo.New(f.db).ListPendingChatAnalysisEvaluations(t.Context(), analysisrepo.ListPendingChatAnalysisEvaluationsParams{
		ProjectID:        f.projectID,
		CursorObservedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageSize:         analysis.PendingCandidatePage,
		Inactivity:       pgtype.Interval{Microseconds: analysis.InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return rows
}

// judgeLimiter builds the shared judge rate limiter over the test Redis.
func judgeLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(client))
}

// stubCompletionClient answers every object completion with a canned body.
type stubCompletionClient struct {
	mu       sync.Mutex
	response string
	calls    int
}

var _ openrouter.CompletionClient = (*stubCompletionClient)(nil)

func (s *stubCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++

	content := or.CreateChatAssistantMessageContentStr(s.response)
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
		Model:        JudgeModel,
		Usage:        openrouter.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      s.response,
	}, nil
}

func (s *stubCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, nil //nolint:nilnil // never exercised by the judge
}

func (s *stubCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, nil //nolint:nilnil // never exercised by the judge
}

func (s *stubCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string, opts ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, nil //nolint:nilnil // never exercised by the judge
}

// captureSkillSink is an in-memory ScoreSink for skill_efficacy_scores.
type captureSkillSink struct {
	mu       sync.Mutex
	inserted []telemetryrepo.SkillEfficacyScore
	existing []string
}

var _ ScoreSink = (*captureSkillSink)(nil)

func (c *captureSkillSink) InsertSkillEfficacyScores(ctx context.Context, rows []telemetryrepo.SkillEfficacyScore) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inserted = append(c.inserted, rows...)
	return nil
}

func (c *captureSkillSink) ListExistingSkillEfficacyScoreIDs(ctx context.Context, arg telemetryrepo.ListExistingSkillEfficacyScoreIDsParams) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.existing))
	copy(out, c.existing)
	return out, nil
}

// captureAnalysisSink is an in-memory analysis.ScoreSink for
// chat_analysis_scores.
type captureAnalysisSink struct {
	mu       sync.Mutex
	inserted []telemetryrepo.ChatAnalysisScore
}

var _ analysis.ScoreSink = (*captureAnalysisSink)(nil)

func (c *captureAnalysisSink) InsertChatAnalysisScores(ctx context.Context, rows []telemetryrepo.ChatAnalysisScore) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inserted = append(c.inserted, rows...)
	return nil
}

func (c *captureAnalysisSink) ListExistingChatAnalysisScoreIDs(ctx context.Context, arg telemetryrepo.ListExistingChatAnalysisScoreIDsParams) ([]string, error) {
	return nil, nil
}
