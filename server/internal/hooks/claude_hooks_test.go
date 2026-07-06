package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// stubBlockingShadowMCPScanner is a RiskScanner that always reports a
// non-nil shadow-MCP blocking policy. Used to exercise the hook deny path
// without standing up the real risk-policy stack.
type stubBlockingShadowMCPScanner struct{}

func (stubBlockingShadowMCPScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return nil, nil
}

func (stubBlockingShadowMCPScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return &risk.ShadowMCPPolicy{ID: "00000000-0000-0000-0000-000000000001", Name: "shadow-mcp-block"}, nil
}

func (stubBlockingShadowMCPScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

type userScopedShadowMCPScanner struct {
	userID string
}

func (s userScopedShadowMCPScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return nil, nil
}

func (s userScopedShadowMCPScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, userID string) (*risk.ShadowMCPPolicy, error) {
	if userID != s.userID {
		return nil, nil
	}
	return &risk.ShadowMCPPolicy{ID: "00000000-0000-0000-0000-000000000001", Name: "shadow-mcp-block"}, nil
}

func (s userScopedShadowMCPScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

func TestNormalizeClaudeHookEvent_PrefersAuthContextProjectOverCachedMetadata(t *testing.T) {
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
		UserEmail: "cached-scan@example.com",
	}, 0))

	normalized, err := ti.service.normalizeClaudeHookEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
	}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, normalized)
	got, ok := normalized.(*hookevents.UserPromptSubmit)
	require.True(t, ok)
	assert.Equal(t, authCtx.ActiveOrganizationID, got.Context.OrganizationID)
	assert.Equal(t, *authCtx.ProjectID, got.Context.ProjectID)
	assert.Empty(t, got.Context.User.ID)
}

func TestNormalizeClaudeHookEvent_AllowsMissingUserEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID: sessionID,
		ProjectID: uuid.NewString(),
		UserEmail: "",
	}, 0))

	normalized, err := ti.service.normalizeClaudeHookEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
	}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, normalized)
	got, ok := normalized.(*hookevents.UserPromptSubmit)
	require.True(t, ok)
	assert.Equal(t, authCtx.ActiveOrganizationID, got.Context.OrganizationID)
	assert.Equal(t, *authCtx.ProjectID, got.Context.ProjectID)
	assert.Empty(t, got.Context.User.ID)
	assert.Empty(t, got.Context.User.Email)
}

func TestNormalizeClaudeHookEvent_ResolvesPayloadEmailBeforeAuthUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	userID := "user_payload_email_scan"
	userEmail := "payload-email-scan@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)

	sessionID := uuid.NewString()
	normalized, err := ti.service.normalizeClaudeHookEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
	}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, normalized)
	got, ok := normalized.(*hookevents.UserPromptSubmit)
	require.True(t, ok)
	assert.Equal(t, authCtx.ActiveOrganizationID, got.Context.OrganizationID)
	assert.Equal(t, *authCtx.ProjectID, got.Context.ProjectID)
	assert.Equal(t, userID, got.Context.User.ID)
	assert.Equal(t, userEmail, got.Context.User.Email)
}

func TestNormalizeClaudeHookEvent_ResolvesAuthContextActorFromCachedEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	userID := "user_cached_email_scan"
	userEmail := "cached-email-scan@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)

	authCtx.UserID = ""
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     userEmail,
		UserID:        "",
		ExternalOrgID: "claude_org",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     uuid.NewString(),
	}, 0))

	normalized, err := ti.service.normalizeClaudeHookEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
	}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, normalized)
	got, ok := normalized.(*hookevents.UserPromptSubmit)
	require.True(t, ok)
	assert.Equal(t, authCtx.ActiveOrganizationID, got.Context.OrganizationID)
	assert.Equal(t, *authCtx.ProjectID, got.Context.ProjectID)
	assert.Equal(t, userID, got.Context.User.ID)
	assert.Equal(t, userEmail, got.Context.User.Email)
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
	userEmail := "claude-authctx@example.com"

	// No MCP list snapshot is cached for this session, so the guard must
	// deny with the retry/restart message. Reaching that check at all proves
	// the auth-context branch ran — before the fix, the empty Redis cache
	// would have returned allow without consulting the policy.
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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
	userEmail := "claude-no-mcp-list@example.com"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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
	userEmail := "claude-non-gram@example.com"

	// Seed the cache with an entry that resolves the tool's server prefix
	// but points at a non-Gram host.
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "plugin", PluginName: "slack", Name: "slack", URL: "https://mcp.slack.com/mcp"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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
	userEmail := "claude-local-stdio@example.com"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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

func TestClaude_PreToolUse_TargetedShadowMCPPolicyUsesResolvedHookUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	hookUserID := "claude-hook-user"
	hookUserEmail := "claude-hook-user@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, hookUserID, hookUserEmail)
	ti.service.riskScanner = userScopedShadowMCPScanner{userID: hookUserID}

	sessionID := uuid.NewString()
	toolName := "mcp__mise__install_tool"
	toolUseID := "toolu_claude_specific_user_policy"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &hookUserEmail,
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

func TestClaude_PreToolUse_DeniesLocalStdioServerWithLegacyIdentityRule(t *testing.T) {
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
	userEmail := "claude-legacy-identity@example.com"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))
	createHookAccessRule(t, ctx, ti, authCtx.ProjectID.String(), accesscontrol.AccessScopeProject, accesscontrol.DispositionAllowed, accesscontrol.MatchKindServerIdentity, "mise mcp", "mise")

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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
	userEmail := "claude-unconfigured@example.com"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "linear", Command: "linear mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))
	createHookAccessRule(t, ctx, ti, authCtx.ProjectID.String(), accesscontrol.AccessScopeProject, accesscontrol.DispositionAllowed, accesscontrol.MatchKindServerIdentity, "github", "GitHub")

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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
	userEmail := "claude-gram-hosted@example.com"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "gram", URL: "https://app.getgram.ai/mcp/team-foo"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
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

// DNO-286: the blocking PreToolUse guard must enforce against the inventory
// carried in the request payload — replayed from the SessionStart inventory
// file by hook.sh — not only the server-side cache. Here no snapshot is
// cached, yet a payload-supplied inventory that resolves the tool's server to
// a Gram-hosted URL must ALLOW, proving the payload path is consulted. Before
// the fix this session would have denied with the retry/restart message
// because the cache races the async SessionStart snapshot.
func TestClaude_PreToolUse_EnforcesFromPayloadInventoryWithoutCache(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_payload_inventory"
	userEmail := "claude-payload-inv@example.com"

	// No cache.Set — the inventory arrives only in the payload.
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "gram: https://app.getgram.ai/mcp/team-foo (HTTP) - connected",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision,
		"payload-supplied inventory must drive enforcement even with no cached snapshot")

	// The payload inventory also self-heals the cache, so the best-effort
	// telemetry annotation path finds the snapshot on subsequent events.
	cached, cacheErr := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, cacheErr, "payload inventory should be written back to the cache")
	require.Len(t, cached, 1)
	assert.Equal(t, "https://app.getgram.ai/mcp/team-foo", cached[0].URL)
}

// A payload-supplied inventory that resolves the server to a non-Gram URL must
// block with the shadow-MCP policy decision — not the "snapshot unavailable"
// retry/restart message — confirming the inventory was consumed for
// enforcement rather than triggering the fail-closed cache-miss branch.
func TestClaude_PreToolUse_PayloadInventoryBlocksNonGramServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__notion__search"
	toolUseID := "toolu_payload_inventory_nongram"
	userEmail := "claude-payload-inv-nongram@example.com"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "notion: https://mcp.notion.com/mcp (HTTP) - connected",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.NotContains(t, *output.PermissionDecisionReason, "restart Claude Code",
		"a payload inventory was supplied, so the block must come from the policy, not the cache-miss fail-closed path")
}

// A payload inventory gathered live this call (mcp_inventory_fresh) must
// supersede a stale cached snapshot. Here the cache resolves "gram" to a
// Gram-hosted URL (would allow), but the fresh payload resolves the same server
// to a non-Gram URL — the fresh inventory must win and BLOCK, and overwrite the
// cache. This is the inline-gather path hook.sh takes on a session's first tool
// call when the SessionStart file does not exist yet.
func TestClaude_PreToolUse_FreshPayloadInventorySupersedesCache(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_fresh_supersedes"
	userEmail := "claude-fresh-supersedes@example.com"

	// Cache holds a Gram-hosted entry that would allow on its own.
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "gram", URL: "https://app.getgram.ai/mcp/team-foo"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "gram: https://shadow.example.com/mcp (HTTP) - connected",
			"mcp_inventory_fresh":       true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision,
		"a live-gathered (fresh) payload inventory must supersede the cached snapshot")

	// The fresh inventory must overwrite the cache, not be discarded.
	cached, cacheErr := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, cacheErr)
	require.Len(t, cached, 1)
	assert.Equal(t, "https://shadow.example.com/mcp", cached[0].URL,
		"fresh inventory should overwrite the previously cached snapshot")
}

// A replayed (non-fresh) payload inventory must NOT override a cached snapshot.
// This guards the ConfigChange race danielkov flagged: the server may already
// hold a fresher inventory (cached synchronously on ConfigChange) while hook.sh
// replays an older per-session file. Here the cache allows (Gram-hosted) and
// the non-fresh replayed payload would block (non-Gram) — the cache must win.
func TestClaude_PreToolUse_StaleReplayDoesNotOverrideCache(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_stale_replay"
	userEmail := "claude-stale-replay@example.com"

	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "gram", URL: "https://app.getgram.ai/mcp/team-foo"}},
		sessionMCPListTTL,
	))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
		AdditionalData: map[string]any{
			// No mcp_inventory_fresh: this is a replay from the per-session file.
			"mcp_inventory_claude_code": "gram: https://shadow.example.com/mcp (HTTP) - connected",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision,
		"a non-fresh replayed inventory must not override the cached snapshot")

	// The cache must remain the Gram-hosted entry, untouched by the replay.
	cached, cacheErr := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, cacheErr)
	require.Len(t, cached, 1)
	assert.Equal(t, "https://app.getgram.ai/mcp/team-foo", cached[0].URL,
		"replayed inventory must not clobber a fresher cached snapshot")
}

// mcpGetErrorCache wraps a real cache but forces a non-miss (transport-style)
// error on Get for one key, so tests can exercise the fail-closed path without
// a flaky real Redis outage.
type mcpGetErrorCache struct {
	cache.Cache
	failKey string
	err     error
}

func (c mcpGetErrorCache) Get(ctx context.Context, key string, value any) error {
	if key == c.failKey {
		return c.err
	}
	//nolint:wrapcheck // test pass-through to the embedded real cache
	return c.Cache.Get(ctx, key, value)
}

// A Redis transport error (not a cache miss) must fail closed even when the
// payload carries a non-fresh replayed inventory. A genuine miss legitimately
// falls back to the replay (DNO-286), but on a transport error we cannot
// establish cache-authoritative ordering, so enforcing against a possibly-stale
// replay is unsafe — deny with the retry/restart message instead.
func TestClaude_PreToolUse_CacheTransportErrorFailsClosedDespiteReplay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_cache_transport_err"
	userEmail := "claude-cache-transport-err@example.com"

	// Force a non-miss error specifically on the MCP list key; everything else
	// (session metadata, auth) still resolves through the real cache.
	ti.service.cache = mcpGetErrorCache{
		Cache:   ti.service.cache,
		failKey: sessionMCPListCacheKey(sessionID),
		err:     errors.New("redis: connection refused"),
	}

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{},
		AdditionalData: map[string]any{
			// Non-fresh replayed inventory (no mcp_inventory_fresh).
			"mcp_inventory_claude_code": "gram: https://app.getgram.ai/mcp/team-foo (HTTP) - connected",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision,
		"a cache transport error must fail closed, not fall back to a non-fresh replay")
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "restart Claude Code",
		"the block must be the cache-unavailable fail-closed message, not a policy decision")
}

func TestMergeClaudeAuthContextMetadata_DoesNotSelectUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	authMetadata, ok := ti.service.claudeAuthContextMetadata(ctx, "session_test", "")
	require.True(t, ok)
	metadata := ti.service.mergeClaudeAuthContextMetadata(ctx, authMetadata, SessionMetadata{
		SessionID:     "session_test",
		ServiceName:   "claude-code",
		UserEmail:     "local-hook-testing@example.com",
		UserID:        "",
		ExternalOrgID: "claude_org",
		GramOrgID:     "org_from_cache",
		ProjectID:     "project_from_cache",
	})

	assert.Empty(t, metadata.UserID)
	assert.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
	assert.Equal(t, authCtx.ProjectID.String(), metadata.ProjectID)
	assert.Equal(t, "claude-code", metadata.ServiceName)
	assert.Equal(t, "local-hook-testing@example.com", metadata.UserEmail)
	assert.Equal(t, "claude_org", metadata.ExternalOrgID)
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
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     localFallbackEmail,
		UserID:        "",
		ExternalOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     cachedProjectID,
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

func TestClaude_RecordHook_DoesNotUseAuthUserIDWhenPayloadEmailDoesNotResolve(t *testing.T) {
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

	assert.Empty(t, msgs[0].UserID.String)
	assert.Equal(t, payloadEmail, msgs[0].ExternalUserID.String)
}

func TestMergeClaudeAuthContextMetadata_DropsCachedUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	authMetadata, ok := ti.service.claudeAuthContextMetadata(ctx, "session_test", "")
	require.True(t, ok)
	metadata := ti.service.mergeClaudeAuthContextMetadata(ctx, authMetadata, SessionMetadata{
		SessionID:     "session_test",
		ServiceName:   "claude-code",
		UserEmail:     "local-hook-testing@example.com",
		UserID:        "user_from_cache",
		ExternalOrgID: "claude_org",
		GramOrgID:     "org_from_cache",
		ProjectID:     "project_from_cache",
	})

	assert.Empty(t, metadata.UserID)
	assert.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
	assert.Equal(t, authCtx.ProjectID.String(), metadata.ProjectID)
	assert.Equal(t, "claude-code", metadata.ServiceName)
	assert.Equal(t, "local-hook-testing@example.com", metadata.UserEmail)
	assert.Equal(t, "claude_org", metadata.ExternalOrgID)
}

// TestMergeClaudeAuthContextMetadata_AdoptsBridgedOwnerForPersonalAccount covers
// the personal-account branch: the session email does not resolve to an org
// member, but the OTEL path already attributed the account to an employee via the
// device bridge (cached AccountType=personal, UserID set). The merge must adopt
// that bridged owner instead of dropping it, and carry the account identity
// through hook re-hydration.
func TestMergeClaudeAuthContextMetadata_AdoptsBridgedOwnerForPersonalAccount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// A gmail that resolves to no org member, so email resolution yields "".
	authMetadata, ok := ti.service.claudeAuthContextMetadata(ctx, "personal_session", "someone@gmail.com")
	require.True(t, ok)
	metadata := ti.service.mergeClaudeAuthContextMetadata(ctx, authMetadata, SessionMetadata{
		SessionID:           "personal_session",
		ServiceName:         "claude-code",
		UserEmail:           "someone@gmail.com",
		UserID:              "bridged-employee",
		Provider:            providerAnthropic,
		ExternalOrgID:       "max-org",
		ExternalAccountUUID: "acct-personal",
		ExternalAccountID:   "user_personal",
		DeviceID:            "device-1",
		AccountType:         accountTypePersonal,
		UserAccountID:       "user-account-id",
		GramOrgID:           "org_from_cache",
		ProjectID:           "project_from_cache",
	})

	// Email didn't resolve, but the cached personal-account owner is adopted.
	assert.Equal(t, "bridged-employee", metadata.UserID)
	assert.Equal(t, accountTypePersonal, metadata.AccountType)
	// Account identity is carried through from the cached OTEL attribution.
	assert.Equal(t, providerAnthropic, metadata.Provider)
	assert.Equal(t, "max-org", metadata.ExternalOrgID)
	assert.Equal(t, "acct-personal", metadata.ExternalAccountUUID)
	assert.Equal(t, "device-1", metadata.DeviceID)
	assert.Equal(t, "user-account-id", metadata.UserAccountID)
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
	assert.Contains(t, *output.PermissionDecisionReason, "/reload-plugins")
	assert.Contains(t, *output.PermissionDecisionReason, "(err code: "+denyCodeNoMetadata+")")
}

func TestClaude_PreToolUse_DeniesMCPWhenResolvedMetadataHasNoUserEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_pretooluse_no_email"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     "",
		UserID:        "",
		ExternalOrgID: "claude_org",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}, 0))

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
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "could not verify this MCP tool call")
	assert.Contains(t, *output.PermissionDecisionReason, "(err code: "+denyCodeNoUserEmail+")")
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
