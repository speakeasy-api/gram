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

// TestIngest_CoworkHooksAndTranscriptStoreEachMessageOnce: in Cowork the
// per-event hooks capture prompts and tool traffic and enforce policy, and the
// transcript shipped from Stop backfills what no hook saw (the first prompt,
// typed in chat before the session moved to Cowork, and assistant text). Both
// key shared messages by prompt_id and tool_use_id, so each message is stored
// once however the paths interleave or repeat.
func TestIngest_CoworkHooksAndTranscriptStoreEachMessageOnce(t *testing.T) {
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
	event := func(eventType, promptID string) *gen.IngestPayload {
		payload := canonicalIngestPayload("claude", eventType, sessionID)
		payload.Session.TranscriptCapture = &captured
		payload.Session.TurnID = &promptID
		return payload
	}

	// Turn 2 runs under the hooks: a prompt, a blocked tool call, a result.
	prompt := "list the files"
	promptEvent := event("prompt.submitted", "prompt-2")
	promptEvent.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	res, err := ti.service.IngestAuthenticated(ctx, authCtx, promptEvent)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	toolID, toolName := "toolu_1", "Bash"
	blocked := event("tool.requested", "prompt-2")
	blocked.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{ID: &toolID, Name: &toolName, Input: map[string]any{"command": "echo BLOCK_TEST_CONTENT"}}}
	res, err = ti.service.IngestAuthenticated(ctx, authCtx, blocked)
	require.NoError(t, err)
	require.Equal(t, "deny", res.Decision, "policy is enforced on Cowork events")

	allowedID := "toolu_2"
	toolEvent := event("tool.requested", "prompt-2")
	toolEvent.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{ID: &allowedID, Name: &toolName, Input: map[string]any{"command": "ls"}}}
	_, err = ti.service.IngestAuthenticated(ctx, authCtx, toolEvent)
	require.NoError(t, err)
	resultEvent := event("tool.completed", "prompt-2")
	resultEvent.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{ID: &allowedID, Name: &toolName, Output: "a.txt"}}
	_, err = ti.service.IngestAuthenticated(ctx, authCtx, resultEvent)
	require.NoError(t, err)

	listMessages := func() []chatRepo.ChatMessage {
		messages, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID})
		require.NoError(t, err)
		return messages
	}
	require.Len(t, listMessages(), 4, "the hooks captured the prompt, both calls (a denied one included, as in Claude Code) and the result")

	// The transcript repeats turn 2 and adds what no hook saw.
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	lines := []string{
		`{"type":"user","uuid":"u-1","promptId":"prompt-1","timestamp":"` + at(-time.Minute) + `","message":{"role":"user","content":"hello from chat"}}`,
		`{"type":"assistant","uuid":"a-1","timestamp":"` + at(-50*time.Second) + `","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Hi there."}]}}`,
		`{"type":"user","uuid":"u-2","promptId":"prompt-2","timestamp":"` + at(0) + `","message":{"role":"user","content":"list the files"}}`,
		`{"type":"assistant","uuid":"a-2","timestamp":"` + at(time.Second) + `","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","uuid":"u-3","promptId":"prompt-2","timestamp":"` + at(2*time.Second) + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"a.txt"}]}}`,
		`{"type":"assistant","uuid":"a-3","timestamp":"` + at(3*time.Second) + `","message":{"id":"m3","role":"assistant","content":[{"type":"text","text":"One file."}]}}`,
	}
	stored := []int64{}
	for range 2 {
		w := claudeTranscriptRequest(t, transcriptHandler, claudeTranscriptUploadBody(sessionID, lines), ahpAuthHeaders)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		stored = append(stored, claudeTranscriptStored(t, w))
	}
	require.Equal(t, []int64{3, 0}, stored, "the transcript adds only the first prompt and the assistant text, once")

	// A late hook for a message the transcript already stored adds nothing.
	_, err = ti.service.IngestAuthenticated(ctx, authCtx, promptEvent)
	require.NoError(t, err)

	messages := listMessages()
	require.Len(t, messages, 7, "one row per message")
	keys := make(map[string]int)
	for _, m := range messages {
		keys[m.ExternalMessageID.String]++
	}
	for key, n := range keys {
		require.Equal(t, 1, n, key)
	}
	require.Contains(t, keys, "claude-prompt:prompt-1", "the chat-phase prompt is backfilled")
	require.Contains(t, keys, "claude-prompt:prompt-2")
	require.Contains(t, keys, "claude-tool-call:toolu_2")
	require.Contains(t, keys, "claude-tool-result:toolu_2")
	require.Contains(t, keys, "claude-tool-call:toolu_1")
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
