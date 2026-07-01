package hooks

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

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

	ti.service.riskScanner = userScopedShadowMCPScanner{userID: authCtx.UserID}

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
	require.Contains(t, *result.Message, shadowMCPApprovalRequestPrompt)
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
