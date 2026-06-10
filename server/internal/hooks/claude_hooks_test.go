package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// stubBlockingShadowMCPScanner is a RiskScanner that always reports a
// non-nil shadow-MCP blocking policy. Used to exercise the hook deny path
// without standing up the real risk-policy stack.
type stubBlockingShadowMCPScanner struct{}

func (stubBlockingShadowMCPScanner) ScanForEnforcement(_ context.Context, _ uuid.UUID, _ string, _ string) (*risk.ScanResult, error) {
	return nil, nil
}

func (stubBlockingShadowMCPScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ uuid.UUID) (*risk.ShadowMCPPolicy, error) {
	return &risk.ShadowMCPPolicy{ID: "stub-policy-id", Name: "shadow-mcp-block"}, nil
}

func (stubBlockingShadowMCPScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

func TestResolveClaudeScanProjectID_PrefersAuthContextOverCachedMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	cachedProjectID := uuid.New()
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID: sessionID,
		ProjectID: cachedProjectID.String(),
	}, 0))

	got, ok := ti.service.resolveClaudeScanProjectID(ctx, sessionID)
	require.True(t, ok)
	assert.Equal(t, *authCtx.ProjectID, got)
}

// When the request authenticated via Gram-Key + Gram-Project, handlePreToolUse
// must build SessionMetadata from the auth context instead of short-circuiting
// to "allow" because Redis hasn't been seeded by OTEL Logs yet. Otherwise the
// shadow-MCP guard never fires on the first PreToolUse of a plugin-driven
// session — exactly when the guard is most needed (no toolset_id present yet
// in the conversation's tool history).
func TestClaude_PreToolUse_UsesAuthContextWhenNoCachedMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	// lookupShadowMCPBlockingPolicy needs a non-nil scanner that reports a
	// blocking shadow-MCP policy, otherwise the handler short-circuits to
	// allow before the cached-MCP-list check runs.
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID, "test setup should populate ProjectID")

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_pretooluse_authctx"

	// No MCP list snapshot is cached for this session, so the guard must
	// deny with the retry/restart message. Reaching that check at all proves
	// the auth-context branch ran — before the fix, the empty Redis cache
	// would have returned allow without consulting the policy.
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be a *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision,
		"shadow-MCP guard must deny when no cached MCP list is available")
}

// When the MCP list snapshot is missing from the cache (SessionStart hasn't
// finished yet, or the 12h inactivity TTL elapsed), the guard fails closed
// and surfaces a retry/restart hint to the user. Failing open would let a
// shadow MCP server slip past during the snapshot-population window.
func TestClaude_PreToolUse_DeniesWhenMCPListNotCached(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_no_mcp_list"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "restart Claude Code",
		"deny reason should tell the user to retry or restart so they aren't stuck guessing")
}

// Gram-hosted MCP servers (URL host == app.getgram.ai) are the only ones
// the shadow-MCP guard permits — even a server present in the cache is
// rejected when its URL points elsewhere.
func TestClaude_PreToolUse_DeniesWhenMatchedServerNotGramHosted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__plugin_slack_slack__send_message"
	toolUseID := "toolu_non_gram_hosted"

	// Seed the cache with an entry that resolves the tool's server prefix
	// but points at a non-Gram host.
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "plugin", PluginName: "slack", Name: "slack", URL: "https://mcp.slack.com/mcp"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
}

// Local stdio MCP servers (no URL — Command-only entries from
// `claude mcp list`) must be denied by the shadow-MCP guard for the same
// reason a non-Gram-hosted HTTP server is: they're not under the org's
// control. The deny reason should name the command so the user knows
// which server to allowlist.
func TestClaude_PreToolUse_DeniesLocalStdioServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__mise__install_tool"
	toolUseID := "toolu_local_stdio"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
}

// Server identity rules are ignored for Shadow MCP access enforcement; local
// stdio servers without URL evidence should still be governed by policy.
func TestClaude_PreToolUse_DoesNotAllowLocalStdioServerByIdentityRule(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	toolName := "mcp__mise__install_tool"
	toolUseID := "toolu_local_stdio_approved"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))
	createHookAccessRule(t, ctx, ti, authCtx.ProjectID.String(), accesscontrol.AccessScopeProject, accesscontrol.DispositionAllowed, accesscontrol.MatchKindServerIdentity, "mise mcp", "mise")

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
}

func TestClaude_PreToolUse_DoesNotAllowUnconfiguredServerByIdentityRule(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	toolName := "mcp__github__search"
	toolUseID := "toolu_unconfigured_identity"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "linear", Command: "linear mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))
	createHookAccessRule(t, ctx, ti, authCtx.ProjectID.String(), accesscontrol.AccessScopeProject, accesscontrol.DispositionAllowed, accesscontrol.MatchKindServerIdentity, "github", "GitHub")

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, `MCP server "github" is not in the active configuration`)
}

// Allow path: a cached entry that resolves the tool's server prefix and
// points at app.getgram.ai must succeed even under a blocking policy.
func TestClaude_PreToolUse_AllowsGramHostedServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_gram_hosted_ok"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "gram", URL: "https://app.getgram.ai/mcp/team-foo"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision)
}

func TestMergeClaudeAuthContextMetadata_PreservesAuthUserIDWhenCacheIsEmpty(t *testing.T) {
	t.Parallel()

	metadata := mergeClaudeAuthContextMetadata(
		SessionMetadata{
			SessionID:   "session_test",
			ServiceName: "",
			UserEmail:   "",
			UserID:      "user_from_auth",
			ClaudeOrgID: "",
			GramOrgID:   "org_from_auth",
			ProjectID:   "project_from_auth",
		},
		SessionMetadata{
			SessionID:   "session_test",
			ServiceName: "claude-code",
			UserEmail:   "local-hook-testing@example.com",
			UserID:      "",
			ClaudeOrgID: "claude_org",
			GramOrgID:   "org_from_cache",
			ProjectID:   "project_from_cache",
		},
	)

	assert.Equal(t, "user_from_auth", metadata.UserID)
	assert.Equal(t, "org_from_auth", metadata.GramOrgID)
	assert.Equal(t, "project_from_auth", metadata.ProjectID)
	assert.Equal(t, "claude-code", metadata.ServiceName)
	assert.Equal(t, "local-hook-testing@example.com", metadata.UserEmail)
	assert.Equal(t, "claude_org", metadata.ClaudeOrgID)
}

func TestClaude_RecordHook_PersistsAuthContextProjectOverCachedMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "hello from auth context project"
	cachedProjectID := uuid.NewString()

	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   localFallbackEmail,
		UserID:      "",
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   cachedProjectID,
	}, time.Hour))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var msgs []chatRepo.ChatMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(msgs) == 1
	}, 2*time.Second, 25*time.Millisecond)

	require.True(t, msgs[0].ProjectID.Valid)
	assert.Equal(t, *authCtx.ProjectID, msgs[0].ProjectID.UUID)
	assert.Equal(t, prompt, msgs[0].Content)
}

func TestClaude_RecordHook_BuffersAuthContextCacheMissWithoutPayloadEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	prompt := "hello before otel metadata"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var buffered []gen.ClaudePayload
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1)
	assert.Equal(t, "UserPromptSubmit", buffered[0].HookEventName)
}

func TestClaude_RecordHook_UsesAuthContextUserIDOnCacheMissWithPayloadEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "hello with payload email"
	payloadEmail := "unknown-user@example.com"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		UserEmail:     &payloadEmail,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var msgs []chatRepo.ChatMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(msgs) == 1
	}, 2*time.Second, 25*time.Millisecond)

	assert.Equal(t, authCtx.UserID, msgs[0].UserID.String)
	assert.Equal(t, payloadEmail, msgs[0].ExternalUserID.String)
}

// When plugin auth headers are present but the API key is invalid/expired,
// Claude() must NOT return a 401 error — that causes the client-side hook
// script to block ALL tool calls, deadlocking the user. Instead it should
// fall through to the same OTEL-buffered path a no-headers request takes:
// the event is buffered in Redis so flushPendingHooks can replay it once
// the session is validated.
func TestClaude_ContinuesWhenPluginAuthFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	badKey := "gram_key_expired_or_invalid"
	projectSlug := "some-project"
	sessionID := uuid.NewString()
	prompt := "hello"

	result, err := ti.service.Claude(t.Context(), &gen.ClaudePayload{
		HookEventName:    "UserPromptSubmit",
		SessionID:        &sessionID,
		ApikeyToken:      &badKey,
		ProjectSlugInput: &projectSlug,
		Prompt:           &prompt,
	})
	require.NoError(t, err, "expired plugin auth must not return an error")
	require.NotNil(t, result)

	// The whole point of the fallback is that the event still lands in
	// the Redis buffer, ready for flushPendingHooks once OTEL Logs seeds
	// the session metadata. Asserting on the buffer (not just NoError)
	// is what catches a regression to the early-return shape.
	var buffered []gen.ClaudePayload
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "hook should be buffered when plugin auth fails")
	require.Equal(t, "UserPromptSubmit", buffered[0].HookEventName)
}

// When Claude PreToolUse cannot resolve org/project metadata for an MCP call,
// fail closed. Buffered telemetry can be replayed later, but it cannot undo an
// already-allowed tool call.
func TestClaude_PreToolUse_DeniesMCPWhenNoAuthAndNoCachedMetadata(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	bareCtx := t.Context()
	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_pretooluse_noauth"

	result, err := ti.service.Claude(bareCtx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision,
		"MCP tool calls without enforcement metadata must fail closed")
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "could not verify this MCP tool call")
}

// Claude Code's hook output schema only permits hookSpecificOutput for
// PreToolUse, PostToolUse, UserPromptSubmit, and PostToolBatch — and even
// those variants need their own required fields. For Stop, SessionStart,
// SessionEnd, Notification, and PostToolUseFailure, including any
// hookSpecificOutput object causes Claude Code to reject the response with
// "Hook JSON output validation failed — (root): Invalid input", which the
// user sees as a Stop hook error. Make sure makeHookResult omits it for those.
func TestClaude_OmitsHookSpecificOutputForNonPreToolUseEvents(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	for _, event := range []string{"Stop", "SessionEnd", "Notification", "PostToolUse", "PostToolUseFailure", "UserPromptSubmit"} {
		t.Run(event, func(t *testing.T) {
			t.Parallel()
			result, err := ti.service.Claude(t.Context(), &gen.ClaudePayload{
				HookEventName: event,
				SessionID:     &sessionID,
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Nil(t, result.HookSpecificOutput,
				"%s response must not include hookSpecificOutput — Claude Code rejects unknown variants", event)
		})
	}
}

// SessionStart is the one non-PreToolUse event that has a meaningful response
// shape (Continue), but it still must NOT carry hookSpecificOutput.
func TestClaude_SessionStart_OmitsHookSpecificOutput(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	sessionID := uuid.NewString()
	result, err := ti.service.Claude(t.Context(), &gen.ClaudePayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.HookSpecificOutput)
	require.NotNil(t, result.Continue)
	assert.True(t, *result.Continue, "SessionStart should always allow the session to continue")
}
