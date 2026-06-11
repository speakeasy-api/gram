package risk_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// fakePromptJudge is a stub ra.PromptJudge that returns a fixed verdict and
// records how many times it was invoked, so tests can assert the scanner only
// calls the judge for messages whose type the policy applies to.
type fakePromptJudge struct {
	verdict *risk_analysis.JudgeVerdict
	calls   atomic.Int32
}

func (f *fakePromptJudge) Evaluate(_ context.Context, _ risk_analysis.JudgeInput) *risk_analysis.JudgeVerdict {
	f.calls.Add(1)
	return f.verdict
}

func insertPromptBasedBlockPolicy(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string, messageTypes []string) {
	t.Helper()
	insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, name, prompt, messageTypes, nil)
}

func insertPromptBasedBlockPolicyWithConfig(t *testing.T, ti *testInstance, ctx context.Context, name, prompt string, messageTypes []string, modelConfig []byte) {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	require.NotNil(t, authCtx.ProjectID)
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
		PolicyType:     "prompt_based",
		Sources:        []string{},
		MessageTypes:   messageTypes,
		Enabled:        true,
		Action:         "block",
		AutoName:       false,
		Prompt:         pgtype.Text{String: prompt, Valid: true},
		ModelConfig:    modelConfig,
	})
	require.NoError(t, err)
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
	judge := &fakePromptJudge{verdict: &risk_analysis.JudgeVerdict{Confidence: 0.9, Rationale: "destructive delete"}}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), ti.conn, nil, nil, judge, promptPoliciesFlag(ctx), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "rm -rf /data", message.ToolRequest)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "block", res.Action)
	require.Equal(t, risk_analysis.SourceLLMJudge, res.Source)
	require.Equal(t, risk_analysis.RuleLLMJudge, res.RuleID)
	require.Equal(t, "destructive delete", res.Description)
}

// TestScanner_PromptBasedPolicyJudgesNonToolMessages verifies a prompt_based
// policy with no message-type restriction is judged inline for non-tool-call
// messages (e.g. a user prompt), not just tool calls.
func TestScanner_PromptBasedPolicyJudgesNonToolMessages(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", nil)
	judge := &fakePromptJudge{verdict: &risk_analysis.JudgeVerdict{Confidence: 1, Rationale: "x"}}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), ti.conn, nil, nil, judge, promptPoliciesFlag(ctx), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "just a user prompt", message.User)
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
	judge := &fakePromptJudge{verdict: &risk_analysis.JudgeVerdict{Confidence: 1, Rationale: "x"}}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), ti.conn, nil, nil, judge, promptPoliciesFlag(ctx), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "just a user prompt", message.User)
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

	scanner, err := risk.NewScanner(testenv.NewLogger(t), ti.conn, nil, nil, judge, promptPoliciesFlag(ctx), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "ls -la", message.ToolRequest)
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, int32(1), judge.calls.Load())
}

func TestScanner_PromptBasedPolicyDisabledWhenFlagOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes", []string{message.ToolRequest})
	judge := &fakePromptJudge{verdict: &risk_analysis.JudgeVerdict{Confidence: 1, Rationale: "x"}}

	scanner, err := risk.NewScanner(testenv.NewLogger(t), ti.conn, nil, nil, judge, &feature.InMemory{}, testenv.NewMeterProvider(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "rm -rf /data", message.ToolRequest)
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

	scanner, err := risk.NewScanner(testenv.NewLogger(t), ti.conn, nil, nil, nil, promptPoliciesFlag(ctx), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	res, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "rm -rf /data", message.ToolRequest)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, risk_analysis.SourceLLMJudge, res.Source)
	require.Equal(t, risk_analysis.RuleLLMJudge, res.RuleID)
	require.Contains(t, res.Description, "fail-closed")
}
