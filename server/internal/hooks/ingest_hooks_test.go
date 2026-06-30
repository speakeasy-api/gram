package hooks

import (
	"testing"

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
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    new("call-1"),
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
