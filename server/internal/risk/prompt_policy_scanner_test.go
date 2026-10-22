package risk_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// fakePromptJudge is a stub promptpolicy.Evaluator that returns a fixed verdict
// and records how many times it was invoked, so tests can assert when the
// scanner reaches the judge.
type fakePromptJudge struct {
	verdict *promptpolicy.Verdict
	err     error
	calls   atomic.Int32
	mu      sync.Mutex
	last    promptpolicy.Input
}

func (f *fakePromptJudge) Evaluate(_ context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.last = in
	f.mu.Unlock()
	return f.verdict, f.err
}

func (f *fakePromptJudge) lastInput() promptpolicy.Input {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func matchedJudgeVerdict(confidence float64, rationale string) *promptpolicy.Verdict {
	return &promptpolicy.Verdict{
		Matched:          true,
		Confidence:       confidence,
		Rationale:        rationale,
		CostUSD:          0,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		STokens:          1,
		Completed:        true,
		Model:            "test-model",
		Provider:         "test-provider",
	}
}

func insertPromptBasedBlockPolicy(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string) string {
	t.Helper()
	return insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, name, prompt, nil)
}

func insertPromptBasedBlockPolicyWithConfig(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string, modelConfig []byte) string {
	t.Helper()
	return insertPromptBasedBlockPolicyWithConfigs(t, ti, ctx, name, prompt, modelConfig, nil)
}

func insertPromptBasedBlockPolicyWithConfigs(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string, modelConfig, analyzerConfig []byte) string {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	require.NotNil(t, authCtx.ProjectID)
	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
		PolicyType:     "prompt_based",
		Sources:        []string{},
		Enabled:        true,
		Action:         "block",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{String: prompt, Valid: true},
		ModelConfig:    modelConfig,
		AnalyzerConfig: analyzerConfig,
	})
	require.NoError(t, err)
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policy.ID.String(),
		},
		Principals: []urn.Principal{authz.AllUsersPrincipal()},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policy.ID.String()),
	}))
	return policy.ID.String()
}

func promptPoliciesFlag(ctx context.Context) *feature.InMemory {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPromptPolicies, authCtx.ActiveOrganizationID, true)
	return flags
}

// TestScanner_PromptBasedPolicyBlocksToolRequest verifies a matching judge
// verdict on a tool-call message produces a block ScanResult sourced to the
// llm_judge.
func TestScanner_PromptBasedPolicyBlocksToolRequest(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes")
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(0.9, "destructive delete")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "block", res.Action)
	require.Equal(t, promptpolicy.Source, res.Source)
	require.Equal(t, promptpolicy.Rule, res.RuleID)
	require.Equal(t, "destructive delete", res.Description)
}

func TestScanner_PubsubRoutesPolicyKeyedPromptJudge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	policyID := insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "no-deletes", "Block destructive deletes", []byte(`{"fail_open":false}`))
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	flags := promptPoliciesFlag(ctx)
	flags.SetFlag(feature.FlagRiskEnforcementPubsub, authCtx.ActiveOrganizationID, true)
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(0.9, "local judge must not run")}
	dispatcher := &fakeEnforcementDispatcher{fn: func(request enforcereply.DispatchRequest) (enforcereply.Outcome, error) {
		require.Len(t, request.Lanes, 1)
		lane := request.Lanes[0]
		require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE, lane.Scanner)
		require.Equal(t, policyID, lane.PolicyID)
		config, ok := request.PromptPolicies[policyID]
		require.True(t, ok)
		require.Equal(t, "Block destructive deletes", config.Prompt)
		require.JSONEq(t, `{"fail_open":false}`, string(config.ModelConfig))
		reply := riskv1.EnforcementReply_builder{
			Scanner:  new(lane.Scanner),
			Status:   new(riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK),
			PolicyId: new(policyID),
			Findings: []*riskv1.EnforcementFinding{riskv1.EnforcementFinding_builder{
				RuleId:   new(promptpolicy.Rule),
				Category: new("prompt_policy"),
				Score:    new(1.0),
			}.Build()},
		}.Build()
		return enforcereply.Outcome{ByLane: map[enforcereply.Lane]*riskv1.EnforcementReply{lane: reply}, Complete: true}, nil
	}}
	scanner, err := risk.NewScannerWithEnforcementDispatcher(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn),
		nil,
		nil,
		promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate),
		flags,
		testCELEngine(t),
		dispatcher,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)
	require.NoError(t, err)

	result, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "delete production", message.User, ""))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, promptpolicy.Source, result.Source)
	require.Equal(t, promptpolicy.Rule, result.RuleID)
	require.Zero(t, judge.calls.Load())
}

func TestScanner_PubsubPromptJudgeFailureHonorsPolicyFailMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		failOpen bool
		blocked  bool
	}{
		{name: "fail closed blocks", failOpen: false, blocked: true},
		{name: "fail open allows", failOpen: true, blocked: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			modelConfig, err := json.Marshal(map[string]any{"fail_open": tc.failOpen})
			require.NoError(t, err)
			insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "no-deletes", "Block destructive deletes", modelConfig)
			authCtx, _ := contextvalues.GetAuthContext(ctx)
			flags := promptPoliciesFlag(ctx)
			flags.SetFlag(feature.FlagRiskEnforcementPubsub, authCtx.ActiveOrganizationID, true)
			dispatcher := &fakeEnforcementDispatcher{fn: func(request enforcereply.DispatchRequest) (enforcereply.Outcome, error) {
				require.Len(t, request.Lanes, 1)
				require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE, request.Lanes[0].Scanner)
				return enforcereply.Outcome{
					ByLane:   map[enforcereply.Lane]*riskv1.EnforcementReply{},
					Failed:   map[enforcereply.Lane]error{request.Lanes[0]: context.DeadlineExceeded},
					Complete: false,
					Deadline: true,
				}, nil
			}}
			scanner, err := risk.NewScannerWithEnforcementDispatcher(
				testenv.NewLogger(t),
				testenv.NewTracerProvider(t),
				testenv.NewMeterProvider(t),
				ti.conn,
				newTestCustomRuleAnalyzer(t, ti.conn),
				nil,
				nil,
				nil,
				flags,
				testCELEngine(t),
				dispatcher,
				metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
			)
			require.NoError(t, err)

			result, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "delete production", message.User, ""))
			require.NoError(t, err)
			if tc.blocked {
				require.NotNil(t, result)
				require.Equal(t, promptpolicy.Rule, result.RuleID)
				require.Contains(t, result.Description, "fail-closed")
			} else {
				require.Nil(t, result)
			}
		})
	}
}

func TestScanner_PubsubSkipsOutOfScopePromptJudgeLane(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	analyzerConfig, err := risk_analysis.WithDetectionScopes(nil, []risk_analysis.DetectionScopeConfig{
		{Category: "prompt_policy", ScopeInclude: `kind == "tool_request"`},
	})
	require.NoError(t, err)
	insertPromptBasedBlockPolicyWithConfigs(t, ti, ctx, "tool requests only", "Block destructive deletes", nil, analyzerConfig)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	flags := promptPoliciesFlag(ctx)
	flags.SetFlag(feature.FlagRiskEnforcementPubsub, authCtx.ActiveOrganizationID, true)
	dispatcher := &fakeEnforcementDispatcher{fn: func(enforcereply.DispatchRequest) (enforcereply.Outcome, error) {
		return enforcereply.Outcome{}, errors.New("out-of-scope prompt judge must not dispatch")
	}}
	scanner, err := risk.NewScannerWithEnforcementDispatcher(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn),
		nil,
		nil,
		nil,
		flags,
		testCELEngine(t),
		dispatcher,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)
	require.NoError(t, err)

	result, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "ordinary user prompt", message.User, ""))
	require.NoError(t, err)
	require.Nil(t, result)
	require.Equal(t, int32(0), dispatcher.calls.Load())
}

// TestScanner_PromptBasedPolicyAttributesMCPTool verifies the judge receives a
// ToolCallMessage with the MCP server/function destructured from the tool name,
// and the raw arguments as the body.
func TestScanner_PromptBasedPolicyAttributesMCPTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-github-writes", "Block writes to the github MCP server")
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	_, err = scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, `{"title":"pwn"}`, message.ToolRequest, "mcp__github__create_issue"))
	require.NoError(t, err)

	msg := judge.lastInput().Message
	require.Equal(t, message.ToolRequest, msg.Type)
	require.Equal(t, "mcp__github__create_issue", msg.ToolName)
	require.Equal(t, "github", msg.MCPServer)
	require.Equal(t, "create_issue", msg.MCPFunction)
	require.JSONEq(t, `{"title":"pwn"}`, msg.Body)
}

// TestScanner_PromptBasedPolicyJudgesNonToolMessages verifies a prompt_based
// policy with no message-type restriction is judged inline for non-tool-call
// messages (e.g. a user prompt), not just tool calls.
func TestScanner_PromptBasedPolicyJudgesNonToolMessages(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes")
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "just a user prompt", message.User, ""))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, int32(1), judge.calls.Load(), "judge must run for non-tool-call messages the policy applies to")
}

// TestScanner_PromptBasedPolicyNoMatch verifies a nil verdict (no match /
// fail-open) does not block.
func TestScanner_PromptBasedPolicyNoMatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes")
	judge := &fakePromptJudge{verdict: nil}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "ls -la", message.ToolRequest, ""))
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, int32(1), judge.calls.Load())
}

func TestScanner_PromptBasedPolicyDisabledWhenFlagOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes")
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), &feature.InMemory{}, testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, int32(0), judge.calls.Load(), "judge must not run while gram-prompt-policies is disabled")
}

func TestScanner_PromptBasedPolicyFailClosedWhenJudgeUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	failClosed := false
	modelConfig, err := json.Marshal(map[string]any{"fail_open": failClosed})
	require.NoError(t, err)
	insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "no-deletes", "Block destructive deletes", modelConfig)

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, nil, promptPoliciesFlag(ctx), testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, promptpolicy.Source, res.Source)
	require.Equal(t, promptpolicy.Rule, res.RuleID)
	require.Contains(t, res.Description, "fail-closed")
}
