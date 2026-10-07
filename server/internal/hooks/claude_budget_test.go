package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// testClaudeBudget stands in for the 5s production budget so the overrun
	// tests answer almost at once.
	testClaudeBudget = 5 * time.Millisecond

	// testClaudeInBudget is the budget for tests whose verdict must land in
	// time. It is long enough that a slow CI run cannot turn the response into
	// the budget fallback.
	testClaudeInBudget = time.Hour

	// testClaudeScanHold is how long a scan is held for the endpoints the
	// budget does not cover: ten times testClaudeBudget, so the Claude budget
	// would fire first if it applied to them.
	testClaudeScanHold = 10 * testClaudeBudget

	// testClaudeBudgetAnswerWithin is how soon an overrun request must be
	// answered: the budget, the posture read and CI scheduling slack. The slow
	// scanner never returns before cleanup, so a response inside this window
	// can only come from the budget fallback.
	testClaudeBudgetAnswerWithin = 3 * time.Second
)

// hooksPostureFeatures enables every product feature except hooks_fail_open,
// which follows failOpen, fails with err when it is set, or panics when panics
// is set. When read is set, each hooks_fail_open lookup signals it without
// blocking.
type hooksPostureFeatures struct {
	failOpen bool
	err      error
	panics   bool
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
	if f.panics {
		panic("hooks_fail_open read panicked")
	}
	if f.err != nil {
		return false, f.err
	}
	return f.failOpen, nil
}

// slowRiskScanner holds every enforcement scan until the test finishes it, or
// for holdFor when that is set, then returns the embedded stub's result, or
// panics when panics is set. It stands in for a risk scan slower than the
// budget.
type slowRiskScanner struct {
	stubResultScanner
	panics      bool
	holdFor     time.Duration
	started     chan struct{}
	startOnce   sync.Once
	release     chan struct{}
	releaseOnce sync.Once
}

func (s *slowRiskScanner) ScanForEnforcement(ctx context.Context, _ risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.startOnce.Do(func() { close(s.started) })
	var held <-chan time.Time // nil when holdFor is unset: hold until finish
	if s.holdFor > 0 {
		held = time.After(s.holdFor)
	}
	select {
	case <-s.release:
	case <-held:
	case <-ctx.Done():
		return nil, fmt.Errorf("held enforcement scan: %w", ctx.Err())
	}
	if s.panics {
		panic("held enforcement scan panicked")
	}
	return s.result, nil
}

func (s *slowRiskScanner) finish() {
	s.releaseOnce.Do(func() { close(s.release) })
}

// newBudgetedClaudeService wires a hooks service whose risk scans outlast a
// short decision budget and then return lateResult, for an organization with
// the given hooks posture. Cleanup finishes the held scans and waits for the
// detached handlers before the test database goes away.
func newBudgetedClaudeService(t *testing.T, posture hooksPostureFeatures, lateResult *risk.ScanResult) (context.Context, *testInstance, *slowRiskScanner) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	scanner := &slowRiskScanner{
		stubResultScanner: stubResultScanner{result: lateResult},
		started:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	ti.service.productFeatures = posture
	ti.service.claudeBudget = testClaudeBudget
	ti.service.riskScanner = scanner
	t.Cleanup(func() {
		scanner.finish()
		ti.service.claudeDrains.Wait()
	})
	return ctx, ti, scanner
}

// budgetPayload builds a legacy Claude event for a fresh session: a prompt for
// UserPromptSubmit, otherwise a Bash tool call.
func budgetPayload(hookEventName string) *gen.ClaudePayload {
	payload := &gen.ClaudePayload{
		HookEventName: hookEventName,
		SessionID:     new(uuid.NewString()),
		UserEmail:     new("budget@example.com"),
	}
	if hookEventName == "UserPromptSubmit" {
		payload.Prompt = new("a prompt whose scan overruns the budget")
		return payload
	}
	payload.ToolName = new("Bash")
	payload.ToolUseID = new("toolu_" + uuid.NewString())
	payload.ToolInput = map[string]any{"command": "cat .env"}
	return payload
}

func blockingScanResult() *risk.ScanResult {
	return &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "secret policy",
		Description: "leaked credential",
	}
}

// requireSignal fails the test unless ch signals within
// testClaudeBudgetAnswerWithin.
func requireSignal(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(testClaudeBudgetAnswerWithin):
		require.FailNow(t, msg)
	}
}

// requireScanStarted proves the verdict was pending on the held scan, so the
// response cannot have been the handler's own verdict.
func requireScanStarted(t *testing.T, scanner *slowRiskScanner) {
	t.Helper()
	requireSignal(t, scanner.started, "the enforcement scan never started")
}

// testToolTimeoutReason is the fail-closed reason for a tool call answered
// from the hooks posture.
const testToolTimeoutReason = "Speakeasy blocked this tool call: its security check did not finish in time. Retry in a moment, and contact your administrator if this keeps happening."

// toolTimeoutDenial is the fail-closed posture answer to a PreToolUse event.
func toolTimeoutDenial() *gen.ClaudeHookResult {
	return &gen.ClaudeHookResult{
		SystemMessage: new(testToolTimeoutReason),
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:            new("PreToolUse"),
			PermissionDecision:       new("deny"),
			PermissionDecisionReason: new(testToolTimeoutReason),
		},
	}
}

// A scan that overruns the budget is answered from the org's hooks posture at
// the budget: fail-open passes (a tool call without pre-approval), fail-closed
// or unreadable blocks with a reason, and a prompt is still persisted.
func TestClaude_DecisionBudget_OverrunAnswersFromPosture(t *testing.T) {
	t.Parallel()
	promptReason := strings.Replace(testToolTimeoutReason, "tool call", "prompt", 1)
	failOpen := hooksPostureFeatures{failOpen: true}
	toolDenied := toolTimeoutDenial()
	cases := []struct {
		name    string
		posture hooksPostureFeatures
		event   string
		want    *gen.ClaudeHookResult
	}{
		{name: "fail-open passes prompt", posture: failOpen, event: "UserPromptSubmit", want: &gen.ClaudeHookResult{}},
		{name: "fail-open passes tool call without pre-approval", posture: failOpen, event: "PreToolUse", want: &gen.ClaudeHookResult{HookSpecificOutput: &HookSpecificOutput{HookEventName: new("PreToolUse")}}},
		{name: "fail-closed blocks tool call with reason", event: "PreToolUse", want: toolDenied},
		{name: "fail-closed blocks prompt with reason", event: "UserPromptSubmit", want: &gen.ClaudeHookResult{Decision: new("block"), Reason: &promptReason}},
		{name: "unreadable posture fails closed", posture: hooksPostureFeatures{failOpen: true, err: errors.New("feature store unavailable")}, event: "PreToolUse", want: toolDenied},
		{name: "panicking posture read fails closed", posture: hooksPostureFeatures{failOpen: true, panics: true}, event: "PreToolUse", want: toolDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti, scanner := newBudgetedClaudeService(t, tc.posture, nil)
			payload := budgetPayload(tc.event)

			began := time.Now()
			result, err := ti.service.Claude(ctx, payload)
			elapsed := time.Since(began)
			require.NoError(t, err)
			requireScanStarted(t, scanner)
			require.GreaterOrEqual(t, elapsed, testClaudeBudget, "the response waits out the budget")
			require.Less(t, elapsed, testClaudeBudgetAnswerWithin, "the response must not wait for the scan")
			require.Equal(t, tc.want, result)
			if payload.Prompt == nil {
				return
			}

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
					ChatID:    sessionIDToUUID(*payload.SessionID),
					ProjectID: *authCtx.ProjectID,
				})
				assert.NoError(c, err)
				assert.Len(c, msgs, 1, "the prompt must be persisted even though its verdict missed the budget")
			}, 5*time.Second, 50*time.Millisecond)
		})
	}
}

// The fail-open posture is read while the verdict is still pending, not once
// the budget fires, so an overrun is answered at the budget and a pass-through
// is marked superseded before a late deny can record its block.
func TestClaude_DecisionBudget_ReadsPostureWhileVerdictPending(t *testing.T) {
	t.Parallel()
	postureRead := make(chan struct{}, 1)
	ctx, ti, scanner := newBudgetedClaudeService(t, hooksPostureFeatures{failOpen: true, read: postureRead}, nil)
	// A budget this long cannot fire during the test, so any posture read it
	// observes overlapped the pending verdict.
	ti.service.claudeBudget = testClaudeInBudget

	responses := make(chan claudeHookVerdict, 1)
	go func() {
		result, err := ti.service.Claude(ctx, budgetPayload("UserPromptSubmit"))
		responses <- claudeHookVerdict{result: result, err: err}
	}()

	requireScanStarted(t, scanner)
	requireSignal(t, postureRead, "the posture must be read while the verdict is pending")

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
		{name: "panicking read", organizationID: organizationID, features: hooksPostureFeatures{failOpen: true, panics: true}, want: false},
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
	ctx, ti, scanner := newBudgetedClaudeService(t, hooksPostureFeatures{failOpen: false}, blockingScanResult())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	began := time.Now()

	result, err := ti.service.Claude(ctx, budgetPayload("PreToolUse"))
	require.NoError(t, err)
	requireScanStarted(t, scanner)
	require.Equal(t, hookMetricDecisionDeny, claudeHookDecision(result), "the response is the timeout block")

	scanner.finish()
	ti.service.claudeDrains.Wait()

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
	ti.service.claudeBudget = testClaudeInBudget
	ti.service.productFeatures = hooksPostureFeatures{failOpen: true}
	ti.service.riskScanner = &stubResultScanner{result: blockingScanResult()}

	result, err := ti.service.Claude(ctx, budgetPayload("UserPromptSubmit"))
	require.NoError(t, err)
	require.Equal(t, "block", conv.PtrValOr(result.Decision, ""))
	require.Contains(t, conv.PtrValOr(result.Reason, ""), "secret policy")
}

// A fast allow keeps its explicit permissionDecision.
func TestClaude_DecisionBudget_FastAllowUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.claudeBudget = testClaudeInBudget
	ti.service.productFeatures = hooksPostureFeatures{failOpen: false}
	ti.service.riskScanner = &stubResultScanner{}

	result, err := ti.service.Claude(ctx, budgetPayload("PreToolUse"))
	require.NoError(t, err)
	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.Equal(t, "allow", conv.PtrValOr(output.PermissionDecision, ""))
}

// A verdict that lands within the budget carries its handler's enforcement
// scan to the request's tracker, which feeds the risk_scanned metric dimension.
func TestClaude_DecisionBudget_FastVerdictReportsRiskScan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.claudeBudget = testClaudeInBudget
	ti.service.riskScanner = &stubResultScanner{}
	payload := budgetPayload("UserPromptSubmit")
	hookEvent, err := ti.service.normalizeClaudeHookEvent(ctx, payload, time.Now())
	require.NoError(t, err)

	ctx, scanned := withRiskScanTracker(ctx)
	_, postureOutcome, err := ti.service.decideClaudeHookWithinBudget(ctx, testenv.NewLogger(t), time.Now(), hookEvent, payload.HookEventName)
	require.NoError(t, err)
	require.Empty(t, postureOutcome, "the handler's verdict answers")
	require.True(t, *scanned, "the handler's scan must reach the request's tracker")
}

// A handler that panics within the budget is answered from the org's hooks
// posture, as an overrun is, instead of failing the request or crashing the
// server, and its metric outcome says it panicked.
func TestClaude_DecisionBudget_HandlerPanicAnswersFromPosture(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		posture hooksPostureFeatures
		want    *gen.ClaudeHookResult
	}{
		{name: "fail-open passes", posture: hooksPostureFeatures{failOpen: true}, want: &gen.ClaudeHookResult{HookSpecificOutput: &HookSpecificOutput{HookEventName: new("PreToolUse")}}},
		{name: "fail-closed blocks", posture: hooksPostureFeatures{failOpen: false}, want: toolTimeoutDenial()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti, scanner := newBudgetedClaudeService(t, tc.posture, nil)
			ti.service.claudeBudget = testClaudeInBudget
			reader := sdkmetric.NewManualReader()
			ti.service.metrics = newMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t))
			scanner.panics = true
			scanner.finish()

			result, err := ti.service.Claude(ctx, budgetPayload("PreToolUse"))
			require.NoError(t, err)
			require.Equal(t, tc.want, result)

			var rm metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(ctx, &rm))
			point := findHookEventDurationPoint(t, rm)
			outcome, _ := point.Attributes.Value(attr.OutcomeKey)
			require.Equal(t, hookMetricOutcomeHandlerPanic, outcome.AsString())
		})
	}
}

// lockedBuffer collects log output written from several goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // bytes.Buffer.Write always returns a nil error
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A handler that panics after the budget answered is logged, and the posture
// answer stands.
func TestClaude_DecisionBudget_LateHandlerPanicIsLogged(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, hooksPostureFeatures{failOpen: true}, nil)
	logs := &lockedBuffer{}
	ti.service.logger = slog.New(slog.NewJSONHandler(logs, nil))
	scanner.panics = true

	result, err := ti.service.Claude(ctx, budgetPayload("UserPromptSubmit"))
	require.NoError(t, err)
	requireScanStarted(t, scanner)
	require.Equal(t, &gen.ClaudeHookResult{}, result, "the fail-open posture answers")

	scanner.finish()
	ti.service.claudeDrains.Wait()
	require.Contains(t, logs.String(), `"msg":"recovered from panic"`)
}

// The decision budget applies only to the legacy Claude endpoint: Codex,
// Cursor and ingest wait out a scan held past it and return its block, where a
// budget answer would be the fail-open pass-through.
func TestDecisionBudget_OtherEndpointsWaitForVerdict(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		call func(context.Context, *Service) error
	}{
		{name: "codex", call: func(ctx context.Context, s *Service) error {
			_, err := s.Codex(ctx, &gen.CodexPayload{HookEventName: "UserPromptSubmit", SessionID: new(uuid.NewString()), UserEmail: new("budget@example.com"), Prompt: new("a slow prompt")})
			return err
		}},
		{name: "cursor", call: func(ctx context.Context, s *Service) error {
			_, err := s.Cursor(ctx, &gen.CursorPayload{HookEventName: "beforeSubmitPrompt", ConversationID: new(uuid.NewString()), UserEmail: new("budget@example.com"), Prompt: new("a slow prompt")})
			return err
		}},
		{name: "ingest", call: func(ctx context.Context, s *Service) error {
			payload := canonicalIngestPayload("claude", "prompt.submitted", uuid.NewString())
			payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: new("a slow prompt")}}
			_, err := s.Ingest(ctx, payload)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti, scanner := newBudgetedClaudeService(t, hooksPostureFeatures{failOpen: true}, blockingScanResult())
			scanner.holdFor = testClaudeScanHold
			reader := sdkmetric.NewManualReader()
			ti.service.metrics = newMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t))

			require.NoError(t, tc.call(ctx, ti.service))

			var rm metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(ctx, &rm))
			point := findHookEventDurationPoint(t, rm)
			outcome, _ := point.Attributes.Value(attr.OutcomeKey)
			require.Equal(t, hookMetricOutcomeAccepted, outcome.AsString())
			decision, _ := point.Attributes.Value(attr.HookDecisionKey)
			require.Equal(t, hookMetricDecisionDeny, decision.AsString(), "the response is the scanner's block")
		})
	}
}

// A block row written for an event already answered as a pass-through would
// claim a block the user never received, so it is skipped.
func TestInsertToolCallBlock_SkipsSupersededVerdict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

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
		ChatID:         chatIDForBlock("session-superseded"),
	})

	_, err = riskRepo.New(ti.conn).GetToolCallBlock(ctx, riskRepo.GetToolCallBlockParams{
		ID:           blockID,
		ViewerUserID: authCtx.UserID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no block row may exist for a superseded verdict")
}
