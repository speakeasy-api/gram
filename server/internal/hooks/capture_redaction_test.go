package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// This fixed synthetic credential exercises the provider rule, not a live key.
const captureSyntheticPAT = "ghp_" + "b847c2d93a5e4f6071cb9368d014a2e75f69"

type failingCaptureInspector struct{}

func (failingCaptureInspector) InspectCapture(context.Context, chat.CaptureInput) ([]*riskv1.Finding, error) {
	return nil, errors.New("synthetic capture preparation failure")
}

func TestCaptureRedactionProtectsCanonicalFanout(t *testing.T) {
	t.Parallel()
	ctx, ti, published := newTeeTestService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	auth := hookAuthContext(t, ctx)
	payload := teeToolPayload("tool.completed", uuid.NewString(), "capture-call", "capture-idempotency")
	text := "結果 🐕\nMISE_GITHUB_TOKEN=" + captureSyntheticPAT + "\nkeep this output"
	payload.Data.ToolCall.Output = map[string]any{"stdout": text}
	payload.Data.ToolCall.Error = map[string]any{"detail": text}
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	ti.service.otelTeeDrains.Wait()
	records := published.records()
	require.Len(t, records, 1)
	require.NotContains(t, teeStringAttr(t, records[0], string(attr.GenAIToolCallResultKey)), captureSyntheticPAT)
	require.NotContains(t, teeStringAttr(t, records[0], string(attr.HookErrorKey)), captureSyntheticPAT)
	rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
		ChatID: sessionIDToUUID(*payload.Session.ID), ProjectID: *auth.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotContains(t, rows[0].Content, captureSyntheticPAT)
	require.Contains(t, rows[0].Content, "keep this output")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		logs, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: auth.ProjectID.String(), TimeStart: time.Now().Add(-time.Minute).UnixNano(),
			TimeEnd: time.Now().Add(time.Minute).UnixNano(), SortOrder: "desc", Limit: 10,
		})
		require.NoError(collect, err)
		require.Len(collect, logs, 1)
		require.NotContains(collect, logs[0].Attributes, captureSyntheticPAT)
	}, 3*time.Second, 25*time.Millisecond)
	require.Equal(t, map[string]any{"stdout": text}, payload.Data.ToolCall.Output, "capture must not rewrite the harness payload")
	require.Equal(t, map[string]any{"detail": text}, payload.Data.ToolCall.Error)
}

func TestCaptureRedactionProtectsPendingBufferWithUnattributedMarkers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	sessionID := uuid.NewString()
	text := "MISE_GITHUB_TOKEN=" + captureSyntheticPAT + " keep this output"
	payload := &gen.ClaudePayload{
		SessionID: &sessionID, HookEventName: "PostToolUse", ToolResponse: map[string]any{"stdout": text},
		Cwd: new("/synthetic/" + captureSyntheticPAT), TranscriptPath: new("/synthetic/" + captureSyntheticPAT + ".jsonl"),
		ToolName: new(captureSyntheticPAT), Model: new(captureSyntheticPAT),
	}
	require.NoError(t, ti.service.bufferHook(ctx, sessionID, payload))
	var buffered []gen.ClaudePayload
	auth := hookAuthContext(t, ctx)
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey(auth.ProjectID.String(), sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1)
	encoded, err := json.Marshal(map[string]any{"response": buffered[0].ToolResponse, "metadata": buffered[0].AdditionalData})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), captureSyntheticPAT)
	require.Contains(t, string(encoded), "keep this output")
	require.Contains(t, string(encoded), "secret.github_pat")
	require.Contains(t, string(encoded), "$strings[")
	require.NotContains(t, string(encoded), "HS256")
	require.Equal(t, map[string]any{"stdout": text}, payload.ToolResponse)
	require.NotContains(t, *buffered[0].Cwd, captureSyntheticPAT)
	require.NotContains(t, *buffered[0].TranscriptPath, captureSyntheticPAT)
	require.NotContains(t, *buffered[0].ToolName, captureSyntheticPAT)
	require.NotContains(t, *buffered[0].Model, captureSyntheticPAT)
	require.Contains(t, *payload.Cwd, captureSyntheticPAT)
	require.NotNil(t, buffered[0].AdditionalData[pendingCaptureRedactionsField])
}

func TestCaptureRedactionProtectsSessionDirectoryCopies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	sessionID := uuid.NewString()
	metadata := testSessionMetadata(t, ctx, sessionID)
	metadata.Cwd = "/synthetic/" + captureSyntheticPAT
	require.NoError(t, ti.service.cacheSessionMetadata(ctx, metadata))
	cached, err := ti.service.getSessionMetadata(ctx, sessionID)
	require.NoError(t, err)
	require.NotContains(t, cached.Cwd, captureSyntheticPAT)
	auth := hookAuthContext(t, ctx)
	chatID := sessionIDToUUID(sessionID)
	require.NoError(t, ti.service.ensureHookChat(ctx, ti.service.repo, &metadata, chatID, *auth.ProjectID, "synthetic capture"))
	stored, err := chatrepo.New(ti.conn).GetChat(ctx, chatrepo.GetChatParams{
		ID: chatID, ProjectID: *auth.ProjectID,
	})
	require.NoError(t, err)
	require.NotContains(t, stored.Cwd.String, captureSyntheticPAT)
	require.Contains(t, metadata.Cwd, captureSyntheticPAT, "capture must preserve the caller's metadata")
}

func TestCaptureRedactionEnforcementSeesOriginal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	scanner := &recordingCursorRiskScanner{}
	ti.service.riskScanner = scanner
	text := "MISE_GITHUB_TOKEN=" + captureSyntheticPAT
	payload := canonicalIngestPayload("codex", "prompt.submitted", uuid.NewString())
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	require.Equal(t, text, scanner.text)
	require.Equal(t, text, *payload.Data.Prompt.Text)
	auth := hookAuthContext(t, ctx)
	rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
		ChatID: sessionIDToUUID(*payload.Session.ID), ProjectID: *auth.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotContains(t, rows[0].Content, captureSyntheticPAT)
}

func TestCaptureRedactionFailurePreservesPolicyDecision(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{"allow", "deny"} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.productFeatures = alwaysEnabledFeatures{}
			ti.service.writer.WithCaptureInspector(failingCaptureInspector{})
			scanner := &recordingCursorRiskScanner{}
			if decision == "deny" {
				scanner.result = &risk.ScanResult{Action: "block", PolicyName: "synthetic policy", Source: "gitleaks"}
			}
			ti.service.riskScanner = scanner
			text := "MISE_GITHUB_TOKEN=" + captureSyntheticPAT
			payload := canonicalIngestPayload("codex", "prompt.submitted", uuid.NewString())
			payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
			result, err := ti.service.Ingest(ctx, payload)
			require.NoError(t, err, "capture failures must not invoke the client fail-open path")
			require.Equal(t, decision, result.Decision)
			require.Equal(t, text, scanner.text)
			auth := hookAuthContext(t, ctx)
			rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
				ChatID: sessionIDToUUID(*payload.Session.ID), ProjectID: *auth.ProjectID,
			})
			require.NoError(t, err)
			require.Empty(t, rows, "capture failure must not persist original content")
		})
	}
}

func TestCaptureRedactionProtectsLegacyBlockedToolTelemetry(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	auth := hookAuthContext(t, ctx)
	sessionID := uuid.NewString()
	text := "MISE_GITHUB_TOKEN=" + captureSyntheticPAT
	payload := &gen.ClaudePayload{
		SessionID: &sessionID, HookEventName: "PreToolUse", ToolName: new("Bash"),
		ToolInput: map[string]any{"command": text},
	}
	metadata := &SessionMetadata{
		SessionID: sessionID, ProjectID: auth.ProjectID.String(), GramOrgID: auth.ActiveOrganizationID,
		UserID: auth.UserID, UserEmail: conv.PtrValOr(auth.Email, ""),
	}
	ti.service.writeClaudeBlockToClickHouse(ctx, payload, metadata, "blocked by policy")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		logs, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: auth.ProjectID.String(), TimeStart: time.Now().Add(-time.Minute).UnixNano(),
			TimeEnd: time.Now().Add(time.Minute).UnixNano(), SortOrder: "desc", Limit: 10,
		})
		require.NoError(collect, err)
		require.Len(collect, logs, 1)
		require.NotContains(collect, logs[0].Attributes, captureSyntheticPAT)
		require.Contains(collect, logs[0].Attributes, "blocked by policy")
	}, 3*time.Second, 25*time.Millisecond)
	require.Equal(t, map[string]any{"command": text}, payload.ToolInput)
}

func TestCaptureRedactionTelemetryUsesWholeEventContext(t *testing.T) {
	t.Parallel()
	// Synthetic pair also used by the detector's format fixtures.
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	attrs := map[attr.Key]any{
		attr.GenAIToolCallArgumentsKey: "AWS_ACCESS_KEY_ID=" + accessID,
		attr.GenAIToolCallResultKey:    "SecretAccessKey=" + secret,
		attr.GenAIUsageInputTokensKey:  int64(9007199254740993),
	}
	got := cloneCaptureAttributes(t.Context(), attrs)
	require.NotContains(t, got[attr.GenAIToolCallResultKey], secret)
	require.Equal(t, attrs[attr.GenAIUsageInputTokensKey], got[attr.GenAIUsageInputTokensKey])
	require.Equal(t, "SecretAccessKey="+secret, attrs[attr.GenAIToolCallResultKey])
}
