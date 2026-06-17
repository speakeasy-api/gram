package telemetry_test

import (
	"context"
	"encoding/json"
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
