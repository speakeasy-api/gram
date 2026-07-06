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
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
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

func TestIngest_DuplicateDeliveryDoesNotMintSecondBlockRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	idempotencyKey := "dup-" + uuid.NewString()
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "canonical-shadow-mcp-dup")
	toolCallID := "call-dup-1"
	payload.IdempotencyKey = &idempotencyKey
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

	first, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", first.Decision)
	require.NotNil(t, first.Message)
	require.Contains(t, *first.Message, "/blocks/")

	retry, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", retry.Decision, "retried delivery must still be denied")
	require.NotNil(t, retry.Message)
	require.NotContains(t, *retry.Message, "/blocks/",
		"a duplicate delivery must not mint a second block row and URL")
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

// TestIngest_InferredSkillEmitsDerivedTelemetryRow covers Codex-style skill
// detection, where the sender attaches data.skill to an ordinary tool event
// instead of reclassifying it: the underlying tool row must stay truthful
// (policy scans and tool counts key on it) and the activation must land as a
// separate skill.activated row matching the Claude vocabulary.
func TestIngest_InferredSkillEmitsDerivedTelemetryRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	raw := "PreToolUse"
	toolName := "Bash"
	toolID := "call_skill_read"
	payload := canonicalIngestPayload("codex", "tool.requested", "codex-skill-session")
	payload.Source.RawEventName = &raw
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolID,
			Name:  &toolName,
			Input: map[string]any{"command": "cat .agents/skills/repo-review/SKILL.md"},
		},
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		logs, err = chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == 2
	}, 2*time.Second, 50*time.Millisecond, "expected the tool row plus a derived skill row")

	byEvent := map[string]telemetryrepo.TelemetryLog{}
	for _, l := range logs {
		switch {
		case strings.Contains(l.Attributes, "skill.activated"):
			byEvent["skill"] = l
		case strings.Contains(l.Attributes, "PreToolUse"):
			byEvent["tool"] = l
		}
	}

	toolRow, ok := byEvent["tool"]
	require.True(t, ok, "the underlying tool event must be recorded with its provider event name")
	require.Contains(t, toolRow.Attributes, `"Bash"`, "the tool row must keep the real tool identity")
	require.NotContains(t, toolRow.Attributes, "skill.activated")

	skillRow, ok := byEvent["skill"]
	require.True(t, ok, "an inferred skill must produce a derived skill.activated row")
	require.Contains(t, skillRow.Attributes, "repo-review")
	require.Contains(t, skillRow.Attributes, `"Skill"`)
	require.NotNil(t, skillRow.GramChatID)
	require.Equal(t, "codex-skill-session", *skillRow.GramChatID)
}

// TestIngest_PromptInferredSkillsGetDistinctTraces: skill dashboards count
// activations at trace level, and prompt events carry no tool call id — the
// session-hash trace fallback would collapse every prompt-mention activation
// in a session into one summary row, so each derived row mints its own trace.
func TestIngest_PromptInferredSkillsGetDistinctTraces(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	for i, promptText := range []string{"use $repo-review on this", "run $repo-review again"} {
		payload := canonicalIngestPayload("codex", "prompt.submitted", "codex-prompt-skill-session")
		payload.Event.OccurredAt = &occurredAt
		key := "prompt-skill-" + uuid.NewString()
		payload.IdempotencyKey = &key
		text := promptText
		payload.Data = &gen.HookIngestData{
			Prompt: &gen.HookPromptData{Text: &text},
			Skill:  &gen.HookSkillData{Name: "repo-review"},
		}
		result, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err, "ingest %d", i)
		require.Equal(t, "allow", result.Decision)
	}

	var skillTraces []string
	require.Eventually(t, func() bool {
		rows, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		if err != nil {
			return false
		}
		skillTraces = skillTraces[:0]
		for _, row := range rows {
			if strings.Contains(row.Attributes, "skill.activated") && row.TraceID != nil {
				skillTraces = append(skillTraces, *row.TraceID)
			}
		}
		return len(skillTraces) == 2
	}, 2*time.Second, 50*time.Millisecond, "expected two derived skill rows")
	require.NotEqual(t, skillTraces[0], skillTraces[1],
		"prompt-inferred activations in one session must not share a trace")
}

// TestIngest_ExplicitSkillActivationEmitsSingleRow pins the other half of the
// derived-row gate: a sender-classified skill.activated event is already the
// skill row and must not spawn a duplicate.
func TestIngest_ExplicitSkillActivationEmitsSingleRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	payload := canonicalIngestPayload("claude", "skill.activated", "claude-skill-session")
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	listRows := func() ([]telemetryrepo.TelemetryLog, error) {
		return chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
	}

	require.Eventually(t, func() bool {
		rows, err := listRows()
		return err == nil && len(rows) >= 1
	}, 2*time.Second, 50*time.Millisecond)
	require.Never(t, func() bool {
		rows, err := listRows()
		return err == nil && len(rows) > 1
	}, 500*time.Millisecond, 100*time.Millisecond,
		"an explicit skill.activated event must not mint a derived duplicate")
	rows, err := listRows()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].Attributes, "skill.activated")
	require.Contains(t, rows[0].Attributes, "repo-review")
}

// TestIngest_BlockedEventDoesNotEmitDerivedSkillRow: a policy-denied tool call
// never ran, so an inferred skill on it is not an activation.
func TestIngest_BlockedEventDoesNotEmitDerivedSkillRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	toolCallID := "call-blocked-skill"
	payload := canonicalIngestPayload("codex", "tool.requested", "codex-blocked-skill-session")
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "cat .agents/skills/repo-review/SKILL.md"},
		},
		Mcp:   &gen.HookMCPData{ServerIdentity: &serverIdentity},
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)

	listRows := func() ([]telemetryrepo.TelemetryLog, error) {
		return chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
	}

	require.Eventually(t, func() bool {
		rows, err := listRows()
		return err == nil && len(rows) >= 1
	}, 2*time.Second, 50*time.Millisecond)
	require.Never(t, func() bool {
		rows, err := listRows()
		if err != nil {
			return false
		}
		for _, row := range rows {
			if strings.Contains(row.Attributes, "skill.activated") {
				return true
			}
		}
		return false
	}, 500*time.Millisecond, 100*time.Millisecond,
		"a blocked event must not produce a derived activation row")
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

// Codex PermissionRequest normalizes to tool.requested but is only a
// pre-approval preview — it may be denied or followed by the real request,
// so it must not create tool_calls rows in the captured transcript.
func TestIngest_PermissionRequestsNotPersistedAsToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-perms-" + uuid.NewString()
	toolName := "shell"
	permissionType := "exec"
	rawEvent := "PermissionRequest"

	payload := canonicalIngestPayload("codex", "tool.requested", sessionID)
	payload.Source.RawEventName = &rawEvent
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			Name:           &toolName,
			Input:          map[string]any{"command": "ls"},
			PermissionType: &permissionType,
		},
	}
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Empty(t, msgs, "permission prompts must not persist chat rows")
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

// The gram.hook.event attribute vocabulary is the provider-style HookEvent
// names — ClickHouse summary predicates match on PostToolUse and friends, so
// canonical event types must translate back before they reach telemetry.
func TestTelemetryHookEventName_TranslatesCanonicalVocabulary(t *testing.T) {
	t.Parallel()

	withRaw := func(adapter, eventType, raw string) *gen.IngestPayload {
		payload := canonicalIngestPayload(adapter, eventType, "vocab-session")
		payload.Source.RawEventName = &raw
		return payload
	}

	// Known adapters resolve through their raw provider event name.
	require.Equal(t, "PostToolUse", telemetryHookEventName(withRaw("claude", "tool.completed", "PostToolUse")))
	require.Equal(t, "PostToolUseFailure", telemetryHookEventName(withRaw("claude", "tool.failed", "PostToolUseFailure")))
	require.Equal(t, "AfterMCPExecution", telemetryHookEventName(withRaw("cursor", "tool.completed", "afterMCPExecution")))
	require.Equal(t, "BeforeMCPExecution", telemetryHookEventName(withRaw("cursor", "tool.requested", "beforeMCPExecution")))
	require.Equal(t, "UserPromptSubmit", telemetryHookEventName(withRaw("claude", "prompt.submitted", "UserPromptSubmit")))
	require.Equal(t, "PermissionRequest", telemetryHookEventName(withRaw("codex", "tool.requested", "PermissionRequest")))

	// Unrecognized raw names for known adapters fall back to the canonical map.
	require.Equal(t, "PreToolUse", telemetryHookEventName(withRaw("cursor", "tool.requested", "beforeReadFile")))

	// Custom adapters have no raw vocabulary: canonical types map to their
	// provider-style equivalents so summaries still count them.
	require.Equal(t, "PostToolUse", telemetryHookEventName(canonicalIngestPayload("openclaw", "tool.completed", "vocab-session")))
	require.Equal(t, "SessionStart", telemetryHookEventName(canonicalIngestPayload("openclaw", "session.started", "vocab-session")))
	require.Equal(t, "AfterAgentThought", telemetryHookEventName(canonicalIngestPayload("openclaw", "assistant.thought", "vocab-session")))

	// Canonical types without a provider-style equivalent pass through.
	require.Equal(t, "usage.reported", telemetryHookEventName(canonicalIngestPayload("openclaw", "usage.reported", "vocab-session")))

	// Skill activation is layered onto an ordinary tool event; the raw
	// provider name must not erase it.
	require.Equal(t, "skill.activated", telemetryHookEventName(withRaw("claude", "skill.activated", "PostToolUse")))
}
