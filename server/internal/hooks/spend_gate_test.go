package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// recordingRiskScanner counts ScanForEnforcement calls so tests can assert
// the spend gate runs BEFORE any risk-policy evaluation.
type recordingRiskScanner struct {
	scans int
}

func (s *recordingRiskScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	s.scans++
	return nil, nil
}

func (s *recordingRiskScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (s *recordingRiskScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *recordingRiskScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _, _ string) bool {
	return false
}

func (s *recordingRiskScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _, _ string) {
}

// seedSpendBlock writes gate state that makes the given emails breach a
// blocking rule. The request path still evaluates both target and rule CEL.
func seedSpendBlock(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, emails ...string) {
	t.Helper()
	ruleURN := "spend_rule:33333333-3333-3333-3333-333333333333:v2"

	require.NoError(t, spendrules.WriteGateRules(ctx, ti.spendGateCache, organizationID, spendrules.GateRules{
		SourceUpdatedAt: time.Now().UTC(),
		Rules: []spendrules.GateRule{{
			RuleURN:     ruleURN,
			RuleName:    "Intern hard limit",
			Action:      spendrules.ActionBlock,
			TargetExpr:  `true`,
			RuleExpr:    `spend_usd >= limit_usd`,
			LimitUSD:    100,
			WarnAtPct:   80,
			WindowKind:  spendrules.WindowMonthly,
			WindowStart: time.Now().UTC().Add(-time.Hour),
			WindowEnd:   time.Now().UTC().AddDate(0, 0, 7),
		}},
	}))
	for _, email := range emails {
		user, err := usersrepo.New(ti.conn).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
			Email:          conv.NormalizeEmail(email),
			OrganizationID: organizationID,
		})
		require.NoError(t, err)
		actor := spendrules.Actor{
			UserID:      user.ID,
			Email:       user.Email,
			DisplayName: "",
			Attrs: celenv.Actor{
				Email:          user.Email,
				DepartmentName: "",
				JobTitle:       "",
				EmployeeType:   "",
				DivisionName:   "",
				CostCenterName: "",
				Groups:         nil,
				Roles:          nil,
				SpendUSD:       0,
				LimitUSD:       0,
				UsedPct:        0,
				WarnAtPct:      0,
			},
		}
		require.NoError(t, spendrules.WriteGateActor(ctx, ti.spendGateCache, organizationID, spendrules.NewGateActor(actor, chrepo.ActorWindowSpendRow{
			Email:       actor.Email,
			DailyCost:   0,
			WeeklyCost:  0,
			MonthlyCost: 100,
		}, time.Now().UTC())))
	}
}

func TestClaude_UserPromptSubmit_SpendGateBlocksBeforeRiskScan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// The Claude path resolves the actor from the payload email, not the API
	// key owner.
	userID := "user_spend_prompt_block"
	userEmail := "spend-prompt-block@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := uuid.NewString()
	prompt := "please write some code"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Decision)
	assert.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Contains(t, *result.Reason, "Intern hard limit")
	assert.Contains(t, *result.Reason, "budget resets")

	assert.Zero(t, scanner.scans, "spend gate must deny before any risk-policy scan runs")
}

func TestClaude_PreToolUse_SpendGateDeniesNativeTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_tool_block"
	userEmail := "spend-tool-block@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_spend_gate_native"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "ls"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "Intern hard limit")
	assert.Contains(t, *output.PermissionDecisionReason, "/blocks/",
		"deny reason should carry the durable block page URL")

	assert.Zero(t, scanner.scans, "spend gate must deny before any risk-policy scan runs")
}

func TestClaude_SpendGateMatchesByEmailIdentifier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userEmail := "blocked-by-email@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "user_spend_email", userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := uuid.NewString()
	prompt := "hello"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	assert.Equal(t, "block", *result.Decision)
}

func TestClaude_SpendGateAllowsUnblockedUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	// A different actor is blocked; the caller must pass through.
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "user_someone_else", "someone-else@example.com")
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, "someone-else@example.com")

	sessionID := uuid.NewString()
	prompt := "hello"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Decision)
}

func TestClaude_SpendGateSkipsUnresolvedIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	// Clear the caller identity: the event context resolves no user, so the
	// gate must fail open rather than guess.
	authCtx.UserID = ""
	authCtx.Email = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	sessionID := uuid.NewString()
	prompt := "hello"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Decision)
}

func TestIngest_SpendGateDeniesClaudePrompt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("claude", "prompt.submitted", "spend-gate-ingest")
	text := "hello"
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	assert.Contains(t, *result.Message, "Intern hard limit")
}

func TestIngest_SpendGateDeniesClaudeToolCallWithBlockURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("claude", "tool.requested", "spend-gate-ingest-tool")
	toolName := "Bash"
	toolCallID := "call-spend-1"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"command": "ls"},
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	assert.Contains(t, *result.Message, "Intern hard limit")
	assert.Contains(t, *result.Message, "/blocks/")
}

func TestIngest_SpendGateDeniesCodexToolCallWithBlockURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("codex", "tool.requested", "spend-gate-ingest-codex")
	toolName := "shell"
	toolCallID := "call-spend-codex-1"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"command": "ls"},
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	assert.Contains(t, *result.Message, "Intern hard limit")
	assert.Contains(t, *result.Message, "/blocks/")
}

func TestIngest_SpendGateDeniesCursorPrompt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("cursor", "prompt.submitted", "spend-gate-ingest-cursor")
	text := "hello"
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	assert.Contains(t, *result.Message, "Intern hard limit")
}

func TestIngest_SpendGateIgnoresOtherAdapters(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("opencode", "prompt.submitted", "spend-gate-opencode")
	text := "hello"
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "allow", result.Decision,
		"spend enforcement covers claude/codex/cursor; opencode passes through pending a product decision")
}

func TestCodex_UserPromptSubmit_SpendGateBlocksBeforeRiskScan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_codex_prompt"
	userEmail := "spend-codex-prompt@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := "codex-spend-prompt-" + uuid.NewString()
	prompt := "please write some code"
	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Decision)
	assert.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Contains(t, *result.Reason, "Intern hard limit")
	assert.Contains(t, *result.Reason, "budget resets")

	assert.Zero(t, scanner.scans, "spend gate must deny before any risk-policy scan runs")
}

func TestCodex_PreToolUse_SpendGateDeniesToolCallWithBlockURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_codex_tool"
	userEmail := "spend-codex-tool@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := "codex-spend-tool-" + uuid.NewString()
	toolName := "shell"
	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"command": "ls"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Decision)
	assert.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Contains(t, *result.Reason, "Intern hard limit")
	assert.Contains(t, *result.Reason, "/blocks/",
		"tool-call spend denies must carry a durable block page link")
}

func TestCodex_PermissionRequest_SpendGateDenies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_codex_perm"
	userEmail := "spend-codex-perm@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := "codex-spend-perm-" + uuid.NewString()
	toolName := "shell"
	permissionType := "exec"
	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName:  "PermissionRequest",
		SessionID:      &sessionID,
		UserEmail:      &userEmail,
		ToolName:       &toolName,
		PermissionType: &permissionType,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Decision)
	assert.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Contains(t, *result.Reason, "Intern hard limit")
}

func TestCodex_SpendGateAllowsUnblockedUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// The caller is a resolvable connected user, and gate rules exist in the
	// cache because a DIFFERENT actor is over budget — so an allow here
	// proves the gate evaluated the caller and passed them, not that it
	// fell through the fail-open path on an unresolved identity or an empty
	// rule set. Mirrors TestClaude_SpendGateAllowsUnblockedUser.
	userID := "user_spend_codex_ok"
	userEmail := "spend-codex-ok@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "user_spend_codex_other", "spend-codex-other@example.com")
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, "spend-codex-other@example.com")

	sessionID := "codex-spend-ok-" + uuid.NewString()
	toolName := "shell"
	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Decision)
}

func TestCursor_PreToolUse_SpendGateDeniesNativeToolWithBlockURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_cursor_tool"
	userEmail := "spend-cursor-tool@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	toolName := "Edit"
	toolUseID := "toolu_spend_cursor_1"
	conversationID := "conv-spend-" + uuid.NewString()
	result, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName:  "preToolUse",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Permission)
	assert.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	assert.Contains(t, *result.UserMessage, "Intern hard limit")
	assert.Contains(t, *result.UserMessage, "/blocks/",
		"tool-call spend denies must carry a durable block page link")

	assert.Zero(t, scanner.scans, "spend gate must deny before any risk-policy scan runs")
}

func TestCursor_BeforeMCPExecution_SpendGateDenies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_cursor_mcp"
	userEmail := "spend-cursor-mcp@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	toolName := "MCP:github_search"
	toolUseID := "toolu_spend_cursor_mcp_1"
	conversationID := "conv-spend-mcp-" + uuid.NewString()
	result, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName:  "beforeMCPExecution",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Permission)
	assert.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	assert.Contains(t, *result.UserMessage, "Intern hard limit")
	assert.Contains(t, *result.UserMessage, "/blocks/")
}

func TestCursor_PreToolUse_SpendGateSkipsMCPTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_cursor_mcp_skip"
	userEmail := "spend-cursor-mcp-skip@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	// MCP-routed calls are spend-gated exactly once, at beforeMCPExecution;
	// preToolUse must keep allowing them so one denied call cannot mint two
	// block rows across the paired events.
	toolName := "MCP:github_search"
	toolUseID := "toolu_spend_cursor_mcp_skip_1"
	result, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName: "preToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		UserEmail:     &userEmail,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	assert.Equal(t, "allow", *result.Permission)
}

func TestCodex_SpendGateRedeliveryDoesNotRemintBlockPage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_codex_retry"
	userEmail := "spend-codex-retry@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	sessionID := "codex-spend-retry-" + uuid.NewString()
	toolName := "shell"
	idempotencyKey := "idem-spend-" + uuid.NewString()
	payload := &gen.CodexPayload{
		HookEventName:  "PreToolUse",
		SessionID:      &sessionID,
		UserEmail:      &userEmail,
		ToolName:       &toolName,
		IdempotencyKey: &idempotencyKey,
	}

	first, err := ti.service.Codex(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, first.Decision)
	assert.Equal(t, "deny", *first.Decision)
	require.NotNil(t, first.Reason)
	assert.Contains(t, *first.Reason, "/blocks/")

	// The redelivery keeps the deny but must not mint a second block row, so
	// its reason carries no fresh block link.
	second, err := ti.service.Codex(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, second.Decision)
	assert.Equal(t, "deny", *second.Decision)
	require.NotNil(t, second.Reason)
	assert.NotContains(t, *second.Reason, "/blocks/",
		"idempotent redelivery must not mint another block page")
}

func TestIngest_SpendGateDeniesCursorToolCallWithBlockURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("cursor", "tool.requested", "spend-gate-ingest-cursor-tool")
	toolName := "Edit"
	toolCallID := "call-spend-cursor-1"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"path": "main.go"},
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	assert.Contains(t, *result.Message, "Intern hard limit")
	assert.Contains(t, *result.Message, "/blocks/")
}

func TestIngest_SpendGateDeniesCodexPromptAndNormalizesAdapterCase(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	// A case-variant adapter slug must not dodge the gate.
	payload := canonicalIngestPayload("Codex", "prompt.submitted", "spend-gate-ingest-codex-prompt")
	text := "hello"
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	assert.Contains(t, *result.Message, "Intern hard limit")
}

func TestCursor_BeforeSubmitPrompt_SpendGateDenies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_cursor_prompt"
	userEmail := "spend-cursor-prompt@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	prompt := "hello"
	conversationID := "conv-spend-prompt-" + uuid.NewString()
	result, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName:  "beforeSubmitPrompt",
		Prompt:         &prompt,
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Permission)
	assert.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	assert.Contains(t, *result.UserMessage, "Intern hard limit")
}

func TestCursor_SpendGateRedeliveryDoesNotRemintBlockPage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user_spend_cursor_retry"
	userEmail := "spend-cursor-retry@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, userEmail)

	toolName := "Edit"
	toolUseID := "toolu_spend_cursor_retry_1"
	idempotencyKey := "idem-spend-cursor-" + uuid.NewString()
	payload := &gen.CursorPayload{
		HookEventName:  "preToolUse",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		UserEmail:      &userEmail,
		IdempotencyKey: &idempotencyKey,
	}

	first, err := ti.service.Cursor(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, first.Permission)
	assert.Equal(t, "deny", *first.Permission)
	require.NotNil(t, first.UserMessage)
	assert.Contains(t, *first.UserMessage, "/blocks/")

	// The redelivery keeps the deny but must not mint a second block row, so
	// its reason carries no fresh block link.
	second, err := ti.service.Cursor(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, second.Permission)
	assert.Equal(t, "deny", *second.Permission)
	require.NotNil(t, second.UserMessage)
	assert.NotContains(t, *second.UserMessage, "/blocks/",
		"idempotent redelivery must not mint another block page")
}
