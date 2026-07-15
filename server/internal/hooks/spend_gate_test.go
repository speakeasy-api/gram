package hooks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
)

// recordingRiskScanner counts ScanForEnforcement calls so tests can assert
// the spend gate runs BEFORE any risk-policy evaluation.
type recordingRiskScanner struct {
	scans int
}

var spendGateSeedMu sync.Mutex

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
	spendGateSeedMu.Lock()
	defer spendGateSeedMu.Unlock()

	state := spendrules.NewGateState(organizationID, nil)
	err := ti.service.cache.Get(ctx, "spend_gate:"+organizationID, &state)
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		require.NoError(t, err)
	}
	if state.Actors == nil {
		state.Actors = map[string]spendrules.GateActor{}
	}
	state.Rules = []spendrules.GateRule{{
		RuleURN:    ruleURN,
		RuleName:   "Intern hard limit",
		Action:     spendrules.ActionBlock,
		TargetExpr: `true`,
		RuleExpr:   `spend_usd >= limit_usd`,
		LimitUSD:   100,
		WarnAtPct:  80,
		WindowKind: spendrules.WindowMonthly,
		WindowEnd:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}
	for _, email := range emails {
		actor := spendrules.Actor{
			UserID:      "",
			Email:       email,
			DisplayName: "",
			Attrs: celenv.Actor{
				Email:          email,
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
		state.SetActor(organizationID, actor)
		state.SetActorWindowSpend(organizationID, actor, chrepo.ActorWindowSpendRow{
			Email:       actor.Email,
			DailyCost:   0,
			WeeklyCost:  0,
			MonthlyCost: 100,
		})
	}
	require.NoError(t, spendrules.WriteGateState(ctx, ti.service.cache, organizationID, state))
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

func TestIngest_SpendGateIgnoresOtherAdapters(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	seedSpendBlock(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.Email)

	payload := canonicalIngestPayload("cursor", "prompt.submitted", "spend-gate-cursor")
	text := "hello"
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "allow", result.Decision,
		"v1 spend enforcement is Claude-only; other adapters pass through")
}
