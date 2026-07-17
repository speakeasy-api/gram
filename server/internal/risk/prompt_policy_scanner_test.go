package risk_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// fakePromptJudge is a stub promptpolicy.Evaluator that returns a fixed verdict and
// records how many times it was invoked, so tests can assert the scanner only
// calls the judge for messages whose type the policy applies to.
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
	}
}

func insertPromptBasedBlockPolicy(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string, messageTypes []string) {
	t.Helper()
	insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, name, prompt, messageTypes, nil)
}

func insertPromptBasedBlockPolicyWithConfig(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string, messageTypes []string, modelConfig []byte) {
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
		MessageTypes:   messageTypes,
		Enabled:        true,
		Action:         "block",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{String: prompt, Valid: true},
		ModelConfig:    modelConfig,
	})
	require.NoError(t, err)
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policy.ID.String(),
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{authz.AllUsersPrincipal()},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policy.ID.String()),
	}))
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

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", []string{message.ToolRequest})
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(0.9, "destructive delete")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "block", res.Action)
	require.Equal(t, promptpolicy.Source, res.Source)
	require.Equal(t, promptpolicy.Rule, res.RuleID)
	require.Equal(t, "destructive delete", res.Description)
}

// TestScanner_PromptBasedPolicyAttributesMCPTool verifies the judge receives a
// ToolCallMessage with the MCP server/function destructured from the tool name,
// and the raw arguments as the body.
func TestScanner_PromptBasedPolicyAttributesMCPTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-github-writes", "Block writes to the github MCP server", []string{message.ToolRequest})
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	_, err = scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, `{"title":"pwn"}`, message.ToolRequest, "mcp__github__create_issue")
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

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", nil)
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "just a user prompt", message.User, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, int32(1), judge.calls.Load(), "judge must run for non-tool-call messages the policy applies to")
}

// TestScanner_PromptBasedPolicyRespectsMessageTypes verifies a prompt_based
// policy restricted to tool_request is not judged for other message types.
func TestScanner_PromptBasedPolicyRespectsMessageTypes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", []string{message.ToolRequest})
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "just a user prompt", message.User, "")
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, int32(0), judge.calls.Load(), "judge must not run for a message type the policy excludes")
}

// TestScanner_PromptBasedPolicyNoMatch verifies a nil verdict (no match /
// fail-open) does not block.
func TestScanner_PromptBasedPolicyNoMatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", []string{message.ToolRequest})
	judge := &fakePromptJudge{verdict: nil}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), promptPoliciesFlag(ctx), testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "ls -la", message.ToolRequest, "")
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, int32(1), judge.calls.Load())
}

func TestScanner_PromptBasedPolicyDisabledWhenFlagOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", []string{message.ToolRequest})
	judge := &fakePromptJudge{verdict: matchedJudgeVerdict(1, "x")}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate), &feature.InMemory{}, testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, "")
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
	insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "no-deletes", "Block destructive deletes", []string{message.ToolRequest}, modelConfig)

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, nil, promptPoliciesFlag(ctx), testCELEngine(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, promptpolicy.Source, res.Source)
	require.Equal(t, promptpolicy.Rule, res.RuleID)
	require.Contains(t, res.Description, "fail-closed")
}
