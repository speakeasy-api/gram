package risk_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth/riskhealthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

var errDrainedCredits = fmt.Errorf("openrouter object completion: %w", &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits})

func newHealthTestScanner(t *testing.T, ti *testInstance, meter *riskhealthtest.Meter, evaluate promptpolicy.Evaluator) *risk.Scanner {
	t.Helper()

	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, promptpolicy.NewScanner(testenv.NewLogger(t), evaluate), &feature.InMemory{}, testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)
	return scanner
}

func policySample(orgID, policyID string, outcome riskhealth.Outcome, reason riskhealth.Reason) riskhealthtest.Sample {
	return riskhealthtest.Sample{OrgID: orgID, Component: riskhealth.ComponentPolicyEvaluation, Outcome: outcome, Reason: reason, PolicyID: policyID}
}

// TestScanner_FailOpenPolicyOutageIsRecordedDegraded covers the silent no-op:
// the judge is down, the fail-open policy lets the event through exactly as
// a clean scan would, and only the health counter says it never ran.
func TestScanner_FailOpenPolicyOutageIsRecordedDegraded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	policyID := insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes")
	meter := riskhealthtest.NewMeter(t)
	judge := &fakePromptJudge{err: errDrainedCredits}
	scanner := newHealthTestScanner(t, ti, meter, judge.Evaluate)

	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.Nil(t, res, "a fail-open policy allows the event while its judge is down")

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policySample(authCtx.ActiveOrganizationID, policyID, riskhealth.OutcomeDegraded, riskhealth.ReasonDependencyUnavailable): 1,
	}, meter.Evaluations(t))
}

// TestScanner_FailClosedPolicyOutageIsRecordedDegraded pins that a block
// produced by the fail-closed mode is not counted as a working policy.
func TestScanner_FailClosedPolicyOutageIsRecordedDegraded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	policyID := insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "no-deletes", "Block destructive deletes", []byte(`{"fail_open":false}`))
	meter := riskhealthtest.NewMeter(t)
	judge := &fakePromptJudge{err: errDrainedCredits}
	scanner := newHealthTestScanner(t, ti, meter, judge.Evaluate)

	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "block", res.Action)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policySample(authCtx.ActiveOrganizationID, policyID, riskhealth.OutcomeDegraded, riskhealth.ReasonDependencyUnavailable): 1,
	}, meter.Evaluations(t))
}

func TestScanner_JudgedPolicyIsRecordedCompleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	policyID := insertPromptBasedBlockPolicy(t, ti, ctx, "no-deletes", "Block destructive deletes")
	meter := riskhealthtest.NewMeter(t)
	clean := matchedJudgeVerdict(0.1, "benign")
	clean.Matched = false
	judge := &fakePromptJudge{verdict: clean}
	scanner := newHealthTestScanner(t, ti, meter, judge.Evaluate)

	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "ls /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.Nil(t, res)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policySample(authCtx.ActiveOrganizationID, policyID, riskhealth.OutcomeCompleted, riskhealth.ReasonNone): 1,
	}, meter.Evaluations(t))
}

// TestScanner_SiblingMatchCancelsWithoutDegrading pins that a policy cut short
// because another policy already blocked is canceled, not an outage. The
// waiting judge returns only once the fan-out cancels it.
func TestScanner_SiblingMatchCancelsWithoutDegrading(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	matchingID := insertPromptBasedBlockPolicy(t, ti, ctx, "matching", "match")
	waitingID := insertPromptBasedBlockPolicy(t, ti, ctx, "waiting", "wait")
	meter := riskhealthtest.NewMeter(t)
	evaluate := func(ctx context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		if in.Prompt == "match" {
			return matchedJudgeVerdict(0.9, "matched"), nil
		}
		<-ctx.Done()
		return nil, fmt.Errorf("judge: %w", ctx.Err())
	}
	scanner := newHealthTestScanner(t, ti, meter, evaluate)

	res, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "rm -rf /data", message.ToolRequest, ""))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, matchingID, res.PolicyID)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policySample(authCtx.ActiveOrganizationID, matchingID, riskhealth.OutcomeCompleted, riskhealth.ReasonNone): 1,
		policySample(authCtx.ActiveOrganizationID, waitingID, riskhealth.OutcomeCanceled, riskhealth.ReasonNone):   1,
	}, meter.Evaluations(t))
}

func TestMCPPolicyScanner_PolicyOutageIsRecordedDegraded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	meter := riskhealthtest.NewMeter(t)
	judge := &fakePromptJudge{err: errDrainedCredits}
	detector := risk.NewMCPPolicyScanner(newHealthTestScanner(t, ti, meter, judge.Evaluate), nil)
	prompt := "Block destructive deletes"
	policy := policycore.Policy{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		PolicyType:     risk_analysis.PolicyTypePromptBased,
		Prompt:         &prompt,
		Action:         "block",
	}

	_, err := detector.ScanMCPPolicy(ctx, policy, risk.MCPScanRequest{
		Text: `{"path":"/data"}`, ToolName: "delete", ToolsetID: "", ServerID: uuid.NewString(), UserID: authCtx.UserID, MessageType: message.ToolRequest,
	})
	require.Error(t, err, "an incomplete source surfaces to the MCP evaluator")

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policySample(authCtx.ActiveOrganizationID, policy.ID.String(), riskhealth.OutcomeDegraded, riskhealth.ReasonDependencyUnavailable): 1,
	}, meter.Evaluations(t))
}
