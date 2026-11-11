package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// transcriptWarnScanner answers ScanForEnforcement like a project with one
// enforcing policy whose action is the configured one: any text containing
// SENSITIVE_TEST_CONTENT matches.
type transcriptWarnScanner struct {
	stubResultScanner
	action   string
	requests []risk.RealtimeScanRequest
}

func (s *transcriptWarnScanner) ScanForEnforcement(_ context.Context, req risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.requests = append(s.requests, req)
	if !strings.Contains(req.Text, "SENSITIVE_TEST_CONTENT") {
		return nil, nil
	}
	userMessage := "%{match} looks like sensitive information."
	return &risk.ScanResult{
		Action:       s.action,
		PolicyID:     "policy-1",
		PolicyName:   "Example PII policy",
		MessageType:  message.User,
		RuleID:       "pii.example",
		UserMessage:  &userMessage,
		MatchedValue: "SENSITIVE_TEST_CONTENT",
		Entity:       "EXAMPLE",
	}, nil
}

func transcriptPromptLine(id string, at time.Time, text string) string {
	content, _ := json.Marshal(text)
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":%q,"message":{"role":"user","content":%s}}`, id, at.UTC().Format(time.RFC3339Nano), content)
}

func transcriptWarnings(t *testing.T, h http.Handler, body map[string]any) claudeTranscriptResult {
	t.Helper()
	w := claudeTranscriptRequest(t, h, body, ahpAuthHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res claudeTranscriptResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	return res
}

func newTranscriptWarnHandler(t *testing.T, action string) (http.Handler, *transcriptWarnScanner) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	ti.service.productFeatures = alwaysEnabledFeatures{}
	scanner := &transcriptWarnScanner{stubResultScanner: stubResultScanner{}, action: action, requests: nil}
	ti.service.riskScanner = scanner
	return ti.service.ahpAuthenticate(http.HandlerFunc(ti.service.serveTranscript)), scanner
}

func TestClaudeTranscriptReturnsWarnForRecentPrompt(t *testing.T) {
	t.Parallel()
	h, scanner := newTranscriptWarnHandler(t, "warn")
	sessionID := uuid.NewString()
	now := time.Now()
	lines := []string{
		transcriptPromptLine("u-clean", now.Add(-time.Second), "sort these names"),
		transcriptPromptLine("u-hit", now, "row: SENSITIVE_TEST_CONTENT"),
		`{"type":"assistant","uuid":"a-1","timestamp":"` + now.UTC().Format(time.RFC3339Nano) + `","message":{"id":"m","role":"assistant","content":[{"type":"text","text":"SENSITIVE_TEST_CONTENT echoed"}]}}`,
	}

	res := transcriptWarnings(t, h, claudeTranscriptUploadBody(sessionID, lines))
	require.Equal(t, int64(3), res.Stored)
	require.Equal(t, []claudeTranscriptWarning{{
		PolicyID:   "policy-1",
		PolicyName: "Example PII policy",
		Message:    "SENSITIVE_TEST_CONTENT looks like sensitive information.",
	}}, res.Warnings)

	// Only the user prompts are evaluated, attributed to the Cowork session.
	require.Len(t, scanner.requests, 2)
	for _, req := range scanner.requests {
		require.Equal(t, message.User, req.MessageType)
		require.Equal(t, sessionID, req.Provenance.ExternalConversationID)
		require.Equal(t, agentVariantCowork, req.Provenance.HookSource)
	}

	// A full re-send stores nothing new, so the user is not warned twice.
	res = transcriptWarnings(t, h, claudeTranscriptUploadBody(sessionID, lines))
	require.Equal(t, int64(0), res.Stored)
	require.Empty(t, res.Warnings)
	require.Len(t, scanner.requests, 2)
}

func TestClaudeTranscriptSkipsOldPrompts(t *testing.T) {
	t.Parallel()
	h, scanner := newTranscriptWarnHandler(t, "warn")
	old := time.Now().Add(-2 * claudeTranscriptWarnWindow)
	lines := []string{transcriptPromptLine("u-old", old, "SENSITIVE_TEST_CONTENT")}

	res := transcriptWarnings(t, h, claudeTranscriptUploadBody(uuid.NewString(), lines))
	require.Equal(t, int64(1), res.Stored)
	require.Empty(t, res.Warnings)
	require.Empty(t, scanner.requests)
}

func TestClaudeTranscriptDoesNotSurfaceBlockMatches(t *testing.T) {
	t.Parallel()
	h, scanner := newTranscriptWarnHandler(t, "block")
	lines := []string{transcriptPromptLine("u-hit", time.Now(), "SENSITIVE_TEST_CONTENT")}

	w := claudeTranscriptRequest(t, h, claudeTranscriptUploadBody(uuid.NewString(), lines), ahpAuthHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "warnings", "no warnings field when nothing warns")
	require.Len(t, scanner.requests, 1)
}

func TestClaudeTranscriptRecentPromptsBound(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := make([]claudeTranscriptRow, 0, claudeTranscriptWarnMaxPrompts+3)
	for i := range claudeTranscriptWarnMaxPrompts + 2 {
		rows = append(rows, claudeTranscriptRow{externalID: fmt.Sprintf("u-%d", i), role: "user", content: "p", toolCallID: "", toolCalls: nil, finishReason: "", model: "", messageID: "", createdAt: now})
	}
	rows = append(rows, claudeTranscriptRow{externalID: "tool", role: "tool", content: "out", toolCallID: "t", toolCalls: nil, finishReason: "", model: "", messageID: "", createdAt: now})

	prompts := claudeTranscriptRecentPrompts(rows, now)
	require.Len(t, prompts, claudeTranscriptWarnMaxPrompts)
	require.Equal(t, fmt.Sprintf("u-%d", claudeTranscriptWarnMaxPrompts+1), prompts[len(prompts)-1].externalID, "the newest prompts are kept")
}

// A prompt a per-event hook already stored was enforced by that hook, so the
// transcript that repeats it at Stop does not warn about it again.
func TestClaudeTranscriptSkipsPromptsAHookStored(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	ti.service.productFeatures = alwaysEnabledFeatures{}
	h := ti.service.ahpAuthenticate(http.HandlerFunc(ti.service.serveTranscript))

	sessionID := uuid.NewString()
	captured := true
	promptID := "prompt-hooked"
	text := "row: SENSITIVE_TEST_CONTENT"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Session.TranscriptCapture = &captured
	payload.Session.TurnID = &promptID
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
	_, err := ti.service.IngestAuthenticated(ctx, authCtx, payload)
	require.NoError(t, err)

	scanner := &transcriptWarnScanner{stubResultScanner: stubResultScanner{}, action: "warn", requests: nil}
	ti.service.riskScanner = scanner
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lines := []string{
		`{"type":"user","uuid":"u-1","promptId":"` + promptID + `","timestamp":"` + now + `","message":{"role":"user","content":"row: SENSITIVE_TEST_CONTENT"}}`,
		`{"type":"assistant","uuid":"a-1","timestamp":"` + now + `","message":{"id":"m","role":"assistant","content":[{"type":"text","text":"Done."}]}}`,
	}
	res := transcriptWarnings(t, h, claudeTranscriptUploadBody(sessionID, lines))
	require.Equal(t, int64(1), res.Stored, "only the assistant text is new")
	require.Empty(t, res.Warnings)
	require.Empty(t, scanner.requests, "the hooked prompt is not scanned again")
}
