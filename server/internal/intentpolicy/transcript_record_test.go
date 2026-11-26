package intentpolicy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

// maskRedactor masks the word "secret" and fails on the word "explode".
type maskRedactor struct{}

func (maskRedactor) Text(_ context.Context, text string) (string, bool, error) {
	if strings.Contains(text, "explode") {
		return "", false, errors.New("scan failed")
	}
	masked := strings.ReplaceAll(text, "secret", "[REDACTED]")
	return masked, masked != text, nil
}

func transcriptContext() intentpolicy.RecordContext {
	return intentpolicy.RecordContext{
		AnalysisID:     uuid.New(),
		OrganizationID: "org",
		ProjectID:      uuid.New(),
		UserID:         "user",
		UserEmail:      "dev@example.com",
		Generation:     2,
		TriggerKind:    intentpolicy.TriggerDashboard,
		RequestedAt:    time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
		EvaluatedAt:    time.Date(2026, 11, 26, 9, 0, 1, 0, time.UTC),
	}
}

func TestTranscriptRecordsMaskAndAttribute(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	rc := transcriptContext()
	assistant := uuid.New()
	rows := []intentpolicy.MessageRow{
		{ID: uuid.New(), Seq: 1, Generation: 2, Role: "user", ToolCalls: nil, Content: "deploy with my secret", ToolCallID: "", Model: "", CreatedAt: time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)},
		{ID: assistant, Seq: 2, Generation: 2, Role: "assistant", ToolCalls: []byte(`[{"id":"c1","type":"function","function":{"name":"mcp__github__create_issue","arguments":"{\"token\":\"secret\"}"}}]`), Content: "", ToolCallID: "", Model: "m", CreatedAt: time.Date(2026, 11, 26, 8, 0, 1, 0, time.UTC)},
		{ID: uuid.New(), Seq: 3, Generation: 2, Role: "tool", ToolCalls: nil, Content: "ok", ToolCallID: "c1", Model: "", CreatedAt: time.Date(2026, 11, 26, 8, 0, 2, 0, time.UTC)},
	}

	records, err := intentpolicy.TranscriptRecords(t.Context(), maskRedactor{}, rc, chatID, rows)
	require.NoError(t, err)
	require.Len(t, records, 3)

	user, call, result := records[0], records[1], records[2]
	require.Equal(t, intentpolicy.TranscriptMessageID(chatID, 2, rows[0].ID).String(), user.GetId())
	require.Equal(t, rc.AnalysisID.String(), user.GetAnalysisId())
	require.Equal(t, "dev@example.com", user.GetActor().GetUserEmail())
	require.Equal(t, "deploy with my [REDACTED]", user.GetContent())
	require.Equal(t, int64(len("deploy with my secret")), user.GetContentBytes())
	require.True(t, user.GetRedacted())
	require.Equal(t, "2026-11-26T08:00:00Z", user.GetCreatedAt())
	require.Equal(t, "2026-11-26T09:00:01Z", user.GetExportedAt())

	require.Equal(t, assistant.String(), call.GetMessageId())
	require.Equal(t, "m", call.GetModel())
	require.Len(t, call.GetToolCalls(), 1)
	tc := call.GetToolCalls()[0]
	require.Equal(t, "c1", tc.GetToolCallId())
	require.Equal(t, "github", tc.GetMcpServer())
	require.JSONEq(t, `{"token":"[REDACTED]"}`, tc.GetArguments())
	require.True(t, call.GetRedacted())

	require.Equal(t, "c1", result.GetToolCallId())
	require.False(t, result.GetRedacted())
}

func TestTranscriptRecordsWithholdOnScanFailure(t *testing.T) {
	t.Parallel()

	rows := []intentpolicy.MessageRow{
		{ID: uuid.New(), Seq: 1, Generation: 2, Role: "assistant", ToolCalls: []byte(`[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"explode\"}"}}]`), Content: "running it", ToolCallID: "", Model: "", CreatedAt: time.Time{}},
	}
	records, err := intentpolicy.TranscriptRecords(t.Context(), maskRedactor{}, transcriptContext(), uuid.New(), rows)
	require.NoError(t, err)
	require.Len(t, records, 1)
	m := records[0]
	require.True(t, m.GetWithheld())
	require.False(t, m.HasContent(), "a failed scan withholds content too")
	require.Len(t, m.GetToolCalls(), 1)
	require.False(t, m.GetToolCalls()[0].HasArguments())
	require.Equal(t, "Bash", m.GetToolCalls()[0].GetToolName())
}

func TestTranscriptRecordsTruncateOnRuneBoundary(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("é", intentpolicy.MaxExportedContentBytes) // 2 bytes each
	rows := []intentpolicy.MessageRow{{ID: uuid.New(), Seq: 1, Generation: 2, Role: "user", ToolCalls: nil, Content: content, ToolCallID: "", Model: "", CreatedAt: time.Time{}}}
	records, err := intentpolicy.TranscriptRecords(t.Context(), maskRedactor{}, transcriptContext(), uuid.New(), rows)
	require.NoError(t, err)
	m := records[0]
	require.True(t, m.GetContentTruncated())
	require.LessOrEqual(t, len(m.GetContent()), intentpolicy.MaxExportedContentBytes)
	require.True(t, strings.HasSuffix(m.GetContent(), "é"))
	require.Equal(t, int64(len(content)), m.GetContentBytes())
}
