package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/riskhealth"
)

// A policy that reaches no verdict because an engine it depends on was down
// still denies under the LLM engine mode, so nothing in the enforcement
// metrics distinguishes it from a real detection. The availability counter
// is where that shows up as an outage.
func TestScanner_PolicyDependencyOutageIsRecordedDegraded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	insertRealtimeEnforcingPolicy(t, ti, ctx, "pii", []string{risk_analysis.SourcePresidio}, []string{"EMAIL_ADDRESS"}, "block")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, authCtx.ActiveOrganizationID, feature.VariantRiskLLMLLM)
	deadLettered := &fakeEnforcementDispatcher{fn: func(request enforcereply.DispatchRequest) (enforcereply.Outcome, error) {
		lane := request.Lanes[0]
		reply := riskv1.EnforcementReply_builder{
			Scanner: new(lane.Scanner),
			Status:  new(riskv1.EnforcementStatus_ENFORCEMENT_STATUS_DEAD_LETTER),
			Reason:  new("disabled"),
		}.Build()
		return enforcereply.Outcome{ByLane: map[enforcereply.Lane]*riskv1.EnforcementReply{lane: reply}, Complete: true}, nil
	}}

	scanner, reader := newMeteredScanner(t, ti, &instrumentedPIIScanner{}, &recordingPIEngine{}, flags, deadLettered)
	result, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(
		authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "alice@example.com", message.User, "",
	))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.AnalysisUnavailable())

	require.Equal(t, map[string]int64{"degraded/dependency_unavailable": 1}, policyEvaluationHealth(t, reader))
}

func TestScanner_CompletedPolicyEvaluationIsRecorded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	insertRealtimeEnforcingPolicy(t, ti, ctx, "secrets", []string{risk_analysis.SourceGitleaks}, nil, "block")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	clean := perLaneDispatcher(nil, func(lane enforcereply.Lane) *riskv1.EnforcementReply { return okReply(lane) })
	scanner, reader := newMeteredScanner(t, ti, &instrumentedPIIScanner{}, &recordingPIEngine{}, pubsubEnforcementFlags(ctx), clean)

	result, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(
		authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "nothing to see here", message.User, "",
	))
	require.NoError(t, err)
	require.Nil(t, result)

	require.Equal(t, map[string]int64{"completed/none": 1}, policyEvaluationHealth(t, reader))
}

// policyEvaluationHealth sums risk.analysis.evaluations for the policy
// evaluation component, keyed by "<outcome>/<reason>".
func policyEvaluationHealth(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))

	counts := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != riskhealth.MeterEvaluations {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				component, ok := dp.Attributes.Value(attribute.Key("gram.risk.component"))
				require.True(t, ok)
				if component.AsString() != string(riskhealth.ComponentPolicyEvaluation) {
					continue
				}
				outcome, ok := dp.Attributes.Value(attribute.Key("gram.outcome"))
				require.True(t, ok)
				reason, ok := dp.Attributes.Value(attribute.Key("gram.risk.degradation_reason"))
				require.True(t, ok)
				counts[outcome.AsString()+"/"+reason.AsString()] += dp.Value
			}
		}
	}
	return counts
}
