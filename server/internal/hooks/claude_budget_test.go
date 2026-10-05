package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// testClaudeDecisionBudget stands in for the 5s production budget so the
	// overrun tests finish quickly.
	testClaudeDecisionBudget = 200 * time.Millisecond

	// testClaudeBudgetAnswerWithin is how soon an overrun request must be
	// answered: the budget, the posture read and CI scheduling slack. The slow
	// scanner never returns before cleanup, so a response inside this window
	// can only come from the budget fallback.
	testClaudeBudgetAnswerWithin = 3 * time.Second
)

// hooksPostureFeatures enables every product feature except hooks_fail_open,
// which follows failOpen, or fails with err when it is set. When read is set,
// each hooks_fail_open lookup signals it without blocking.
type hooksPostureFeatures struct {
	failOpen bool
	err      error
	read     chan struct{}
}

func (f hooksPostureFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	if feature != productfeatures.FeatureHooksFailOpen {
		return true, nil
	}
	if f.read != nil {
		select {
		case f.read <- struct{}{}:
		default:
		}
	}
	if f.err != nil {
		return false, f.err
	}
	return f.failOpen, nil
}

// slowRiskScanner holds every enforcement scan until the test finishes it,
// then returns result. It stands in for a risk scan slower than the budget.
type slowRiskScanner struct {
	result      *risk.ScanResult
	started     chan struct{}
	startOnce   sync.Once
	release     chan struct{}
	releaseOnce sync.Once
}

func newSlowRiskScanner(result *risk.ScanResult) *slowRiskScanner {
	return &slowRiskScanner{
		result:  result,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *slowRiskScanner) ScanForEnforcement(ctx context.Context, _ risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.startOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("held enforcement scan: %w", ctx.Err())
	}
}

func (s *slowRiskScanner) finish() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func (s *slowRiskScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (s *slowRiskScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *slowRiskScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _, _ string) bool {
	return false
}

func (s *slowRiskScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _, _ string) {
}

// newBudgetedClaudeService wires a hooks service whose risk scans outlast a
// short decision budget, for an organization with the given fail-open
// posture. Cleanup finishes the held scans and waits for the detached
// handlers before the test database goes away.
func newBudgetedClaudeService(t *testing.T, failOpen bool, lateResult *risk.ScanResult) (context.Context, *testInstance, *slowRiskScanner) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = hooksPostureFeatures{failOpen: failOpen}
	ti.service.claudeDecisionBudget = testClaudeDecisionBudget
	scanner := newSlowRiskScanner(lateResult)
	ti.service.riskScanner = scanner
	t.Cleanup(func() {
		scanner.finish()
		ti.service.claudeDecisionDrains.Wait()
	})
	return ctx, ti, scanner
}

func blockingScanResult() *risk.ScanResult {
	return &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "secret policy",
		Description: "leaked credential",
	}
}

// requireScanStarted proves the verdict was pending on the held scan, so the
// response cannot have been the handler's own verdict.
func requireScanStarted(t *testing.T, scanner *slowRiskScanner) {
	t.Helper()
	select {
	case <-scanner.started:
	case <-time.After(testClaudeBudgetAnswerWithin):
		require.FailNow(t, "the enforcement scan never started")
	}
}

// A fail-open organization gets the pass-through answer a legacy client treats
// as success once the scan overruns the budget, and the prompt is still
// persisted although its verdict never arrived.
func TestClaude_DecisionBudget_FailOpenPassesPromptAndPersistsIt(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, true, blockingScanResult())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	prompt := "a prompt whose scan overruns the budget"
	userEmail := "budget-fail-open@example.com"

	began := time.Now()
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		UserEmail:     &userEmail,
	})
	elapsed := time.Since(began)
	require.NoError(t, err)
	require.NotNil(t, result)
	requireScanStarted(t, scanner)
	require.GreaterOrEqual(t, elapsed, testClaudeDecisionBudget, "the response waits out the budget")
	require.Less(t, elapsed, testClaudeBudgetAnswerWithin, "the response must not wait for the scan")
	require.Nil(t, result.Decision, "fail-open must not block the prompt")
	require.Nil(t, result.Reason)
	require.Nil(t, result.Continue)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    sessionIDToUUID(sessionID),
			ProjectID: *authCtx.ProjectID,
		})
		assert.NoError(c, err)
		assert.Len(c, msgs, 1, "the prompt must be persisted even though its verdict missed the budget")
	}, 5*time.Second, 50*time.Millisecond)
}

// A fail-open tool call passes through without a permissionDecision: "allow"
// would skip the user's permission prompt for a call the server never
// verified.
func TestClaude_DecisionBudget_FailOpenPassesToolCallWithoutPreApproval(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, true, blockingScanResult())

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_budget_fail_open"
	userEmail := "budget-fail-open-tool@example.com"

	began := time.Now()
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "cat .env"},
	})
	elapsed := time.Since(began)
	require.NoError(t, err)
	require.NotNil(t, result)
	requireScanStarted(t, scanner)
	require.Less(t, elapsed, testClaudeBudgetAnswerWithin)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.Equal(t, "PreToolUse", *output.HookEventName)
	require.Nil(t, output.PermissionDecision, "fail-open must neither deny nor pre-approve the call")
	require.Nil(t, output.PermissionDecisionReason)
	require.Nil(t, result.SystemMessage)
	require.Nil(t, result.Decision)
}

// A fail-closed organization still blocks, but within the budget and with a
// reason the user can read, instead of the client timing out on HTTP 000.
func TestClaude_DecisionBudget_FailClosedBlocksToolCallWithReason(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, false, nil)

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_budget_fail_closed"
	userEmail := "budget-fail-closed-tool@example.com"

	began := time.Now()
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "ls"},
	})
	elapsed := time.Since(began)
	require.NoError(t, err)
	require.NotNil(t, result)
	requireScanStarted(t, scanner)
	require.Less(t, elapsed, testClaudeBudgetAnswerWithin)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	require.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	require.Contains(t, *output.PermissionDecisionReason, "security check did not finish in time")
	require.NotNil(t, result.SystemMessage, "the user sees the reason in the terminal")
	require.Contains(t, *result.SystemMessage, "this tool call")
}

func TestClaude_DecisionBudget_FailClosedBlocksPromptWithReason(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, false, nil)

	sessionID := uuid.NewString()
	prompt := "a prompt whose scan overruns the budget"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	requireScanStarted(t, scanner)
	require.NotNil(t, result.Decision)
	require.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	require.Contains(t, *result.Reason, "Speakeasy blocked this prompt: its security check did not finish in time")
}

// An organization whose fail-open setting cannot be read is answered as
// fail-closed: a pass-through could let through an event its organization
// chose to block.
func TestClaude_DecisionBudget_UnreadablePostureFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, true, nil)
	ti.service.productFeatures = hooksPostureFeatures{failOpen: true, err: errors.New("feature store unavailable")}

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_budget_unreadable_posture"
	userEmail := "budget-unreadable-posture@example.com"

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
	requireScanStarted(t, scanner)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	require.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	require.Contains(t, *output.PermissionDecisionReason, "security check did not finish in time")
}

// The fail-open posture is read while the verdict is still pending, not once
// the budget fires, so an overrun is answered at the budget and a pass-through
// is marked superseded before a late deny can record its block.
func TestClaude_DecisionBudget_ReadsPostureWhileVerdictPending(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, true, nil)
	// A budget this long cannot fire during the test, so any posture read it
	// observes overlapped the pending verdict.
	ti.service.claudeDecisionBudget = time.Hour
	postureRead := make(chan struct{}, 1)
	ti.service.productFeatures = hooksPostureFeatures{failOpen: true, read: postureRead}

	sessionID := uuid.NewString()
	prompt := "a prompt whose verdict is still pending"
	type claudeResponse struct {
		result *gen.ClaudeHookResult
		err    error
	}
	responses := make(chan claudeResponse, 1)
	go func() {
		result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
			HookEventName: "UserPromptSubmit",
			SessionID:     &sessionID,
			Prompt:        &prompt,
		})
		responses <- claudeResponse{result: result, err: err}
	}()

	requireScanStarted(t, scanner)
	select {
	case <-postureRead:
	case <-time.After(testClaudeBudgetAnswerWithin):
		require.FailNow(t, "the posture must be read while the verdict is pending")
	}

	scanner.finish()
	response := <-responses
	require.NoError(t, response.err)
	require.NotNil(t, response.result)
	require.Nil(t, response.result.Decision, "the handler's own allow answers")
}

// hooksFailOpen reports fail-open only for a setting it read as enabled.
func TestHooksFailOpen_TrueOnlyForReadEnabledSetting(t *testing.T) {
	t.Parallel()
	organizationID := uuid.NewString()
	cases := []struct {
		name           string
		organizationID string
		features       ProductFeaturesClient
		want           bool
	}{
		{name: "enabled setting", organizationID: organizationID, features: hooksPostureFeatures{failOpen: true}, want: true},
		{name: "disabled setting", organizationID: organizationID, features: hooksPostureFeatures{failOpen: false}, want: false},
		{name: "failed read", organizationID: organizationID, features: hooksPostureFeatures{failOpen: true, err: errors.New("feature store unavailable")}, want: false},
		{name: "unknown organization", organizationID: "", features: hooksPostureFeatures{failOpen: true}, want: false},
		{name: "no feature client", organizationID: organizationID, features: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &Service{logger: testenv.NewLogger(t), productFeatures: tc.features}
			require.Equal(t, tc.want, s.hooksFailOpen(t.Context(), tc.organizationID))
		})
	}
}

// A handler that overruns the budget keeps running after the response, so a
// late block is still recorded for a fail-closed organization, where the call
// really was blocked.
func TestClaude_DecisionBudget_LateBlockStillRecordedWhenFailClosed(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, false, blockingScanResult())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_budget_late_block"
	userEmail := "budget-late-block@example.com"
	began := time.Now()

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "cat .env"},
	})
	require.NoError(t, err)
	requireScanStarted(t, scanner)
	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.Contains(t, *output.PermissionDecisionReason, "did not finish in time", "the response is the timeout block")

	scanner.finish()
	ti.service.claudeDecisionDrains.Wait()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := repo.New(ti.conn).ListToolCallBlockSurfaceEvidence(ctx, repo.ListToolCallBlockSurfaceEvidenceParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectIds:     []uuid.UUID{*authCtx.ProjectID},
			FromTime:       pgtype.Timestamptz{Time: began.Add(-time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
			ToTime:         pgtype.Timestamptz{Time: time.Now().Add(time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
		})
		assert.NoError(c, err)
		var blocks int64
		for _, row := range rows {
			blocks += row.BlockCount
		}
		assert.Equal(c, int64(1), blocks, "the late policy block must still be recorded")
	}, 5*time.Second, 50*time.Millisecond)
}

// A fast verdict is returned as before: the policy block reason, not the
// timeout fallback.
func TestClaude_DecisionBudget_FastBlockUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = hooksPostureFeatures{failOpen: true}
	ti.service.riskScanner = &stubResultScanner{result: blockingScanResult()}

	sessionID := uuid.NewString()
	prompt := "here is a secret"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	require.Contains(t, *result.Reason, "secret policy")
	require.NotContains(t, *result.Reason, "did not finish in time")
}

// A fast allow keeps its explicit permissionDecision.
func TestClaude_DecisionBudget_FastAllowUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = hooksPostureFeatures{failOpen: false}
	ti.service.riskScanner = &stubResultScanner{}

	sessionID := uuid.NewString()
	toolName := "Read"
	toolUseID := "toolu_budget_fast_allow"
	userEmail := "budget-fast-allow@example.com"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"file_path": "/tmp/x"},
	})
	require.NoError(t, err)
	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	require.Equal(t, "allow", *output.PermissionDecision)
}

// A block row written for an event already answered as a pass-through would
// claim a block the user never received, so it is skipped.
func TestInsertToolCallBlock_SkipsSupersededVerdict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	supersededCtx, superseded := withVerdictSupersededFlag(ctx)
	superseded.Store(true)
	blockID, err := uuid.NewV7()
	require.NoError(t, err)

	ti.service.insertToolCallBlock(supersededCtx, blockID, toolCallBlockParams{
		Provider:       "claude",
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Reason:         "late block for a call that already passed",
		ToolName:       "Bash",
		UserID:         authCtx.UserID,
		RiskPolicyID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock("session-superseded"),
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	_, err = riskRepo.New(ti.conn).GetToolCallBlock(ctx, riskRepo.GetToolCallBlockParams{
		ID:           blockID,
		ViewerUserID: authCtx.UserID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no block row may exist for a superseded verdict")
}
