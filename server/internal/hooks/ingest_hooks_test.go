package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ingestUserScopedShadowMCPScanner reports a blocking shadow-MCP policy for a
// single user. Unlike userScopedShadowMCPScanner it returns a policy without
// an ID: these tests read the persisted tool_call_blocks row back, and a
// made-up policy UUID would fail the row's risk_policies reference.
type ingestUserScopedShadowMCPScanner struct {
	userID string
}

func (s ingestUserScopedShadowMCPScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return nil, nil
}

func (s ingestUserScopedShadowMCPScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, userID string) (*risk.ShadowMCPPolicy, error) {
	if userID != s.userID {
		return nil, nil
	}
	return &risk.ShadowMCPPolicy{Name: "shadow-mcp-block"}, nil
}

func (s ingestUserScopedShadowMCPScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

func requireBlockIDFromMessage(t *testing.T, message string) uuid.UUID {
	t.Helper()
	const marker = "/blocks/"
	index := strings.LastIndex(message, marker)
	require.NotEqual(t, -1, index, "block message must include %q", marker)
	fields := strings.Fields(message[index+len(marker):])
	require.NotEmpty(t, fields, "block message must include an id after %q", marker)
	blockID, err := uuid.Parse(fields[0])
	require.NoError(t, err)
	return blockID
}

func TestIngest_AcceptsCustomHookSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	sessionID := "custom-ingest-source"

	result, err := ti.service.Ingest(ctx, canonicalIngestPayload("openclaw", "session.started", sessionID))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
}

func TestIngest_RequiresCurrentSchemaVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := canonicalIngestPayload("openclaw", "session.started", "bad-schema")
	payload.SchemaVersion = "hook.ingest.v0"

	result, err := ti.service.Ingest(ctx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "unsupported hook schema_version")
}

func TestIngest_ShadowMCPPolicyUsesAuthenticatedTokenOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "canonical-shadow-mcp")
	toolCallID := "call-1"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	require.Contains(t, *result.Message, "/blocks/")
	blockID := requireBlockIDFromMessage(t, *result.Message)

	var block riskRepo.GetToolCallBlockRow
	require.Eventually(t, func() bool {
		var err error
		block, err = riskRepo.New(ti.conn).GetToolCallBlock(ctx, riskRepo.GetToolCallBlockParams{
			ID:           blockID,
			ViewerUserID: authCtx.UserID,
		})
		return err == nil
	}, 2*time.Second, 25*time.Millisecond)
	require.Equal(t, *authCtx.ProjectID, block.ProjectID)
	require.Equal(t, "search", block.ToolName.String)
	require.Equal(t, authCtx.UserID, block.UserID)
}

func TestCanonicalShadowMCPEvidence_PrefersStdioCommand(t *testing.T) {
	t.Parallel()

	toolName := "mcp__mutable_alias__search"
	serverName := "mutable-alias"
	command := "npx -y @modelcontextprotocol/server-linear"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "canonical-shadow-mcp-command")
	payload.Data = &gen.HookIngestData{
		Mcp: &gen.HookMCPData{
			ServerName: &serverName,
			Command:    &command,
		},
	}

	evidence := canonicalShadowMCPEvidence(payload, toolName)
	require.Equal(t, command, evidence.ServerIdentity)
}

func TestCanonicalChatTitle_TruncatesByRunes(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("界", 100)
	payload := canonicalIngestPayload("custom-adapter", "prompt.submitted", "unicode-title")
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	title := canonicalChatTitle(payload, "")
	require.True(t, utf8.ValidString(title))
	require.Len(t, []rune(title), 80)
}

func TestIngest_SkillActivationIsAcceptedAsFeatureEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := canonicalIngestPayload("claude", "skill.activated", "skill-session")
	payload.Data = &gen.HookIngestData{
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
}

func TestIngest_ThoughtEventsExcludedFromTranscript(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-thought-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)

	text := "internal reasoning about the task"
	role := "assistant"
	thoughtPayload := canonicalIngestPayload("cursor", "assistant.thought", sessionID)
	thoughtPayload.Data = &gen.HookIngestData{
		Message: &gen.HookMessageData{Text: &text, Role: &role},
	}
	res, err := ti.service.Ingest(ctx, thoughtPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// Same data shape as assistant.responded, which does persist — proving
	// the exclusion is keyed on the event type, not on missing content.
	responsePayload := canonicalIngestPayload("cursor", "assistant.responded", sessionID)
	responsePayload.Data = &gen.HookIngestData{
		Message: &gen.HookMessageData{Text: &text, Role: &role},
	}
	res, err = ti.service.Ingest(ctx, responsePayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1, "thought events must not be persisted as chat messages")
	require.Equal(t, "assistant", msgs[0].Role)
}

func TestIngest_PersistsRenderableToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-tools-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	toolCallID := "call_" + uuid.NewString()
	toolName := "Read"

	prompt := "read the file"
	promptPayload := canonicalIngestPayload("custom-adapter", "prompt.submitted", sessionID)
	promptTurnID := "turn-prompt"
	promptPayload.Session.TurnID = &promptTurnID
	promptPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}
	res, err := ti.service.Ingest(ctx, promptPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	requestPayload := canonicalIngestPayload("custom-adapter", "tool.requested", sessionID)
	requestTurnID := "turn-tool-request"
	requestPayload.Session.TurnID = &requestTurnID
	requestPayload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"file_path": "/tmp/input.txt"},
		},
	}
	res, err = ti.service.Ingest(ctx, requestPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	resultPayload := canonicalIngestPayload("custom-adapter", "tool.completed", sessionID)
	resultTurnID := "turn-tool-result"
	resultPayload.Session.TurnID = &resultTurnID
	resultPayload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:     &toolCallID,
			Name:   &toolName,
			Output: map[string]any{"content": "ok"},
		},
	}
	res, err = ti.service.Ingest(ctx, resultPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	var toolRequest, toolResult chatRepo.ChatMessage
	for _, msg := range msgs {
		require.Zero(t, msg.Generation, "hook turn IDs must not split chat.load generations")
		switch {
		case msg.Role == "assistant" && len(msg.ToolCalls) > 0:
			toolRequest = msg
		case msg.Role == "tool":
			toolResult = msg
		}
	}
	require.NotEmpty(t, toolRequest.ID)
	require.Equal(t, "tool_calls", toolRequest.FinishReason.String)
	require.Equal(t, "custom-adapter", toolRequest.Source.String)

	var toolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	require.NoError(t, json.Unmarshal(toolRequest.ToolCalls, &toolCalls))
	require.Len(t, toolCalls, 1)
	require.Equal(t, toolCallID, toolCalls[0].ID)
	require.Equal(t, "function", toolCalls[0].Type)
	require.Equal(t, toolName, toolCalls[0].Function.Name)
	require.JSONEq(t, `{"file_path":"/tmp/input.txt"}`, toolCalls[0].Function.Arguments)

	require.NotEmpty(t, toolResult.ID)
	require.Equal(t, "tool", toolResult.Role)
	require.Equal(t, toolCallID, toolResult.ToolCallID.String)
	require.JSONEq(t, `{"content":"ok"}`, toolResult.Content)
	require.Equal(t, "custom-adapter", toolResult.Source.String)
}

func canonicalIngestPayload(adapter, eventType, sessionID string) *gen.IngestPayload {
	return &gen.IngestPayload{
		SchemaVersion: hookIngestSchemaV1,
		Source: &gen.HookIngestSource{
			Adapter: adapter,
		},
		Session: &gen.HookIngestSession{
			ID: &sessionID,
		},
		Event: &gen.HookIngestEvent{
			Type: eventType,
		},
	}
}
