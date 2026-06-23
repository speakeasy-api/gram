package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

func TestCodex_PreToolUse_ShadowMCPBlockWithIdentityEvidenceIncludesRequestLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := "codex-session-blocked"
	toolName := "mcp__gram__do_thing"
	userEmail := "anonymous-codex@example.com"

	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	require.Contains(t, *result.Reason, "Request access:")
	require.Contains(t, *result.Reason, "/risk-policy-bypass/request#request_token=rpbr1.")
	require.Contains(t, *result.Reason, shadowMCPApprovalRequestPrompt)
}

func TestCodex_PreToolUse_TargetedShadowMCPPolicyUsesResolvedHookUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	hookUserID := "codex-hook-user"
	hookUserEmail := "codex-hook-user@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, hookUserID, hookUserEmail)
	ti.service.riskScanner = userScopedShadowMCPScanner{userID: hookUserID}

	sessionID := "codex-session-specific-user-policy"
	toolName := "mcp__gram__do_thing"
	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &hookUserEmail,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "deny", *result.Decision)
}

func TestCodex_RequiresUserEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "codex-session-missing-email"
	toolName := "mcp__gram__do_thing"
	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.Error(t, err)
	require.Nil(t, result)
	require.ErrorContains(t, err, "codex hook payload missing user_email")
}

func TestCodex_UserPromptSubmit_ScansViaHookEvents(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scanner := &recordingCursorRiskScanner{
		result: &risk.ScanResult{
			PolicyName:  "prompt policy",
			Description: "blocked prompt",
		},
	}
	ti.service.riskScanner = scanner

	sessionID := "codex-session-risk-scan"
	userEmail := "dev@example.com"
	prompt := "do something risky"

	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	require.Contains(t, *result.Reason, "prompt policy")

	require.Equal(t, prompt, scanner.text)
	require.Equal(t, message.User, scanner.messageType)
	require.Empty(t, scanner.toolName)
}

func TestBuildCodexTelemetryAttributes_UsesPayloadUserEmail(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	email := "dev@example.com"
	payload := &gen.CodexPayload{
		HookEventName: "PreToolUse",
		UserEmail:     &email,
	}
	metadata := &SessionMetadata{
		SessionID:   "",
		ServiceName: "Codex",
		UserEmail:   email,
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   "org-id",
		ProjectID:   "project-id",
	}

	// User identity travels on LogParams.UserInfo, not the attributes map.
	attrs := ti.service.buildCodexTelemetryAttributes(t.Context(), payload, metadata)
	require.NotContains(t, attrs, attr.UserEmailKey)
}

func TestCodex_SessionStart_CapturesMCPInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "codex-session-with-inventory"
	email := "dev@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &email,
		AdditionalData: map[string]any{
			"mcp_inventory_codex": []any{
				map[string]any{
					"name":    "int-linear",
					"enabled": true,
					"transport": map[string]any{
						"type": "streamable_http",
						"url":  "https://chat.example.com/mcp/int-linear",
					},
					"auth_status": "o_auth",
				},
			},
		},
	})
	require.NoError(t, err)

	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "int-linear", entries[0].Name)
	require.Equal(t, "https://chat.example.com/mcp/int-linear", entries[0].URL)
}

func TestBuildCodexTelemetryAttributes_EnrichesMCPToolFromInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "codex-session-telemetry-inventory"
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID), []MCPServerEntry{{
		RawLine:       "",
		Source:        "local",
		PluginName:    "",
		Name:          "int-linear",
		URL:           "https://chat.example.com/mcp/int-linear",
		Command:       "",
		Transport:     "HTTP",
		Status:        "unknown",
		StatusRaw:     "o_auth",
		ConnectorUUID: "",
		ToolPrefix:    "int_linear",
	}}, sessionMCPListTTL))

	toolName := "mcp__int_linear__get_issue"
	attrs := ti.service.buildCodexTelemetryAttributes(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
	}, &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "Codex",
		UserEmail:   "",
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   "org-id",
		ProjectID:   "project-id",
	})
	require.Equal(t, "https://chat.example.com/mcp/int-linear", attrs[attr.MCPServerURLKey])
	require.Equal(t, "int-linear", attrs[attr.ToolCallSourceKey])
	require.Equal(t, "get_issue", attrs[attr.ToolNameKey])
}

func TestCodexShadowMCPEvidence_ResolvesURLFromInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "codex-session-evidence-inventory"
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID), []MCPServerEntry{{
		RawLine:       "",
		Source:        "local",
		PluginName:    "",
		Name:          "int-linear",
		URL:           "https://chat.example.com/mcp/int-linear",
		Command:       "",
		Transport:     "HTTP",
		Status:        "unknown",
		StatusRaw:     "o_auth",
		ConnectorUUID: "",
		ToolPrefix:    "int_linear",
	}, {
		RawLine:       "",
		Source:        "local",
		PluginName:    "",
		Name:          "local-tool",
		URL:           "",
		Command:       "npx -y some-server",
		Transport:     "STDIO",
		Status:        "unknown",
		StatusRaw:     "",
		ConnectorUUID: "",
		ToolPrefix:    "local_tool",
	}}, sessionMCPListTTL))

	toolName := "mcp__int_linear__get_issue"
	evidence, matched := ti.service.codexShadowMCPEvidence(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
	})
	require.Equal(t, "int_linear", evidence.ServerIdentity)
	require.Equal(t, "https://chat.example.com/mcp/int-linear", evidence.FullURL)
	require.NotNil(t, matched)

	stdioTool := "mcp__local_tool__run"
	evidence, matched = ti.service.codexShadowMCPEvidence(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &stdioTool,
	})
	require.Equal(t, "npx -y some-server", evidence.ServerIdentity, "stdio identity must pin to the launch command so bypass grants do not follow renamed aliases")
	require.Empty(t, evidence.FullURL)
	require.NotNil(t, matched)

	unknownTool := "mcp__unknown__do_thing"
	evidence, matched = ti.service.codexShadowMCPEvidence(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &unknownTool,
	})
	require.Equal(t, "unknown", evidence.ServerIdentity)
	require.Empty(t, evidence.FullURL)
	require.Nil(t, matched)
}

func TestCodexInventoryProvenanceDetail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	external := &MCPServerEntry{
		RawLine:       "",
		Source:        "local",
		PluginName:    "",
		Name:          "evil",
		URL:           "https://mcp.attacker.example/mcp",
		Command:       "",
		Transport:     "HTTP",
		Status:        "unknown",
		StatusRaw:     "",
		ConnectorUUID: "",
		ToolPrefix:    "",
	}
	detail := ti.service.codexInventoryProvenanceDetail(ctx, external, "org-id")
	require.Contains(t, detail, "not Gram-hosted")
	require.Contains(t, detail, "https://mcp.attacker.example/mcp")

	stdio := &MCPServerEntry{
		RawLine:       "",
		Source:        "local",
		PluginName:    "",
		Name:          "local-tool",
		URL:           "",
		Command:       "npx -y some-server",
		Transport:     "STDIO",
		Status:        "unknown",
		StatusRaw:     "",
		ConnectorUUID: "",
		ToolPrefix:    "local_tool",
	}
	detail = ti.service.codexInventoryProvenanceDetail(ctx, stdio, "org-id")
	require.Contains(t, detail, "local stdio server")

	gramHosted := &MCPServerEntry{
		RawLine:       "",
		Source:        "local",
		PluginName:    "",
		Name:          "gram",
		URL:           "https://app.getgram.ai/mcp/acme",
		Command:       "",
		Transport:     "HTTP",
		Status:        "unknown",
		StatusRaw:     "",
		ConnectorUUID: "",
		ToolPrefix:    "",
	}
	require.Empty(t, ti.service.codexInventoryProvenanceDetail(ctx, gramHosted, "org-id"))

	require.Empty(t, ti.service.codexInventoryProvenanceDetail(ctx, nil, "org-id"))
}

func TestCodexSessionMetadata_CachesSessionStartEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "codex-session-with-email"
	email := "dev@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &email,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	metadata := ti.service.codexSessionMetadata(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
	require.Equal(t, email, metadata.UserEmail)
	require.Equal(t, authCtx.UserID, metadata.UserID)
}

func TestCodexSessionMetadata_IgnoresCachedUserIDWhenEmailDoesNotResolve(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "codex-session-with-stale-cache"
	email := "cached@example.com"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "Codex",
		UserEmail:   email,
		UserID:      "cached-user-id",
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}, 0))

	metadata := ti.service.codexSessionMetadata(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, email, metadata.UserEmail)
	require.Empty(t, metadata.UserID)
}
