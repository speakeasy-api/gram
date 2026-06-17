package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestListClaudeUserPromptCandidatesForCorrelationRanksFuzzyMatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	chatID := uuid.New().String()
	sessionID := uuid.New().String()
	now := time.Now().UTC()

	insertClaudeUserPromptEvent(t, ctx, ti.chClient, claudeUserPromptEventFixture{
		projectID:     projectID,
		chatID:        chatID,
		sessionID:     sessionID,
		promptID:      "prompt-best",
		prompt:        "please summarize the repository changes and include only the important implementation details",
		eventSequence: 1,
		timestamp:     now,
	})
	insertClaudeUserPromptEvent(t, ctx, ti.chClient, claudeUserPromptEventFixture{
		projectID:     projectID,
		chatID:        chatID,
		sessionID:     sessionID,
		promptID:      "prompt-second",
		prompt:        "please summarize repository changes and include only the important implementation details",
		eventSequence: 2,
		timestamp:     now.Add(time.Second),
	})
	insertClaudeUserPromptEvent(t, ctx, ti.chClient, claudeUserPromptEventFixture{
		projectID:     projectID,
		chatID:        chatID,
		sessionID:     sessionID,
		promptID:      "prompt-outside-window",
		prompt:        "please summarize the repository changes and include only important implementation details",
		eventSequence: 3,
		timestamp:     now.Add(20 * time.Minute),
	})

	var candidates []telemetryRepo.ClaudeUserPromptCandidate
	require.Eventually(t, func() bool {
		var err error
		candidates, err = ti.chClient.ListClaudeUserPromptCandidatesForCorrelation(ctx, telemetryRepo.ListClaudeUserPromptCandidatesForCorrelationParams{
			GramProjectID:          projectID,
			GramChatID:             chatID,
			SessionID:              sessionID,
			MessagePrompt:          "please summarize the repository changes and include only important implementation details",
			MessageTimeUnixNano:    now.UnixNano(),
			AfterEventSequence:     0,
			AfterEventTimeUnixNano: 0,
			MinFuzzyLength:         40,
			MaxTimeDeltaNanos:      (10 * time.Minute).Nanoseconds(),
		})
		return err == nil && len(candidates) == 2 && candidates[0].PromptID == "prompt-best"
	}, 2*time.Second, 50*time.Millisecond)

	require.False(t, candidates[0].IsExact)
	require.Greater(t, candidates[0].Similarity, candidates[1].Similarity)
}

func TestListClaudeUserPromptCandidatesForCorrelationBindsLargePrompt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	candidates, err := ti.chClient.ListClaudeUserPromptCandidatesForCorrelation(ctx, telemetryRepo.ListClaudeUserPromptCandidatesForCorrelationParams{
		GramProjectID:          uuid.NewString(),
		GramChatID:             uuid.NewString(),
		SessionID:              uuid.NewString(),
		MessagePrompt:          strings.Repeat("Here is a production example for serviceMainCategories.\nKeep tabs\tand literal JSON escapes like \\n intact. ", 10_000),
		MessageTimeUnixNano:    time.Now().UTC().UnixNano(),
		AfterEventSequence:     0,
		AfterEventTimeUnixNano: 0,
		MinFuzzyLength:         40,
		MaxTimeDeltaNanos:      (10 * time.Minute).Nanoseconds(),
	})
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestListClaudeUserPromptCandidatesForCorrelationMatchesEscapedPrompt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	chatID := uuid.New().String()
	sessionID := uuid.New().String()
	now := time.Now().UTC()
	prompt := `Review this output with \"quotes\", backslashes \\\\, tabs\tand\nnewlines. Keep literal JSON escapes like \\n intact.`

	insertClaudeUserPromptEvent(t, ctx, ti.chClient, claudeUserPromptEventFixture{
		projectID:     projectID,
		chatID:        chatID,
		sessionID:     sessionID,
		promptID:      "prompt-escaped",
		prompt:        prompt,
		eventSequence: 1,
		timestamp:     now,
	})

	var candidates []telemetryRepo.ClaudeUserPromptCandidate
	require.Eventually(t, func() bool {
		var err error
		candidates, err = ti.chClient.ListClaudeUserPromptCandidatesForCorrelation(ctx, telemetryRepo.ListClaudeUserPromptCandidatesForCorrelationParams{
			GramProjectID:          projectID,
			GramChatID:             chatID,
			SessionID:              sessionID,
			MessagePrompt:          prompt,
			MessageTimeUnixNano:    now.UnixNano(),
			AfterEventSequence:     0,
			AfterEventTimeUnixNano: 0,
			MinFuzzyLength:         40,
			MaxTimeDeltaNanos:      (10 * time.Minute).Nanoseconds(),
		})
		return err == nil && len(candidates) == 1 && candidates[0].PromptID == "prompt-escaped"
	}, 2*time.Second, 50*time.Millisecond)

	require.GreaterOrEqual(t, candidates[0].Similarity, 0.95)
}

type claudeUserPromptEventFixture struct {
	projectID     string
	chatID        string
	sessionID     string
	promptID      string
	prompt        string
	eventSequence int64
	timestamp     time.Time
}

func insertClaudeUserPromptEvent(t *testing.T, ctx context.Context, queries *telemetryRepo.Queries, event claudeUserPromptEventFixture) {
	t.Helper()

	attrs, err := json.Marshal(map[string]any{
		"event.name":     "user_prompt",
		"event.sequence": event.eventSequence,
		"session.id":     event.sessionID,
		"prompt.id":      event.promptID,
		"prompt":         event.prompt,
	})
	require.NoError(t, err)

	err = queries.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         event.timestamp.UnixNano(),
		ObservedTimeUnixNano: event.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "claude_code.user_prompt",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrs),
		ResourceAttributes:   "{}",
		GramProjectID:        event.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "claude-code:otel:logs",
		ServiceName:          "claude-code",
		ServiceVersion:       nil,
		GramChatID:           &event.chatID,
	})
	require.NoError(t, err)
}
