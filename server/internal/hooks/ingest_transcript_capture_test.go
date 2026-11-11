package hooks

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// blockMarkerScanner blocks any scanned text containing BLOCK_TEST_CONTENT.
type blockMarkerScanner struct {
	stubResultScanner
}

func (s *blockMarkerScanner) ScanForEnforcement(_ context.Context, req risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	if !strings.Contains(req.Text, "BLOCK_TEST_CONTENT") {
		return nil, nil
	}
	return &risk.ScanResult{Action: "block", PolicyID: "policy-block", PolicyName: "Example block policy", Description: "matched example content"}, nil
}

// TestIngest_TranscriptCaptureEnforcesWithoutStoringRows: a Cowork session
// sends per-event hooks for policy and its transcript for capture. The events
// are still enforced, but only the transcript writes chat rows, so every
// message is stored once however the two paths interleave or repeat.
func TestIngest_TranscriptCaptureEnforcesWithoutStoringRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = &blockMarkerScanner{stubResultScanner: stubResultScanner{}}
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	transcriptHandler := ti.service.ahpAuthenticate(http.HandlerFunc(ti.service.serveTranscript))

	sessionID := uuid.NewString()
	captured := true
	event := func(eventType string) *gen.IngestPayload {
		payload := canonicalIngestPayload("claude", eventType, sessionID)
		payload.Session.TranscriptCapture = &captured
		return payload
	}

	prompt := "list the files"
	promptEvent := event("prompt.submitted")
	promptEvent.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	res, err := ti.service.IngestAuthenticated(ctx, authCtx, promptEvent)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	toolID, toolName := "toolu_1", "Bash"
	blocked := event("tool.requested")
	blocked.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{ID: &toolID, Name: &toolName, Input: map[string]any{"command": "echo BLOCK_TEST_CONTENT"}}}
	res, err = ti.service.IngestAuthenticated(ctx, authCtx, blocked)
	require.NoError(t, err)
	require.Equal(t, "deny", res.Decision, "policy is still enforced on a transcript-captured event")

	listMessages := func() []chatRepo.ChatMessage {
		messages, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID})
		require.NoError(t, err)
		return messages
	}
	require.Empty(t, listMessages(), "per-event hooks store no rows for a transcript-captured session")

	now := time.Now().UTC()
	lines := []string{
		transcriptPromptLine("u-1", now, prompt),
		`{"type":"assistant","uuid":"a-1","timestamp":"` + now.Add(time.Second).Format(time.RFC3339Nano) + `","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Here they are."}]}}`,
	}
	for range 2 {
		w := claudeTranscriptRequest(t, transcriptHandler, claudeTranscriptUploadBody(sessionID, lines), ahpAuthHeaders)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	// Another per-event hook after the transcript still adds nothing.
	_, err = ti.service.IngestAuthenticated(ctx, authCtx, promptEvent)
	require.NoError(t, err)

	messages := listMessages()
	require.Len(t, messages, 2, "one row per transcript entry, none from the events or the re-send")
	for _, m := range messages {
		require.Equal(t, claudeTranscriptOrigin, m.Origin.String)
	}
}

// Without transcript_capture the event is the capture path, as before.
func TestIngest_EventWithoutTranscriptCaptureStoresRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	sessionID := uuid.NewString()
	prompt := "list the files"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err := ti.service.IngestAuthenticated(ctx, authCtx, payload)
	require.NoError(t, err)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}
