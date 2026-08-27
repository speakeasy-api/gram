package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRiskTelemetryUsesOnlyBoundedAttributes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := NewRiskTelemetry(testenv.NewLogger(t), provider)

	telemetry.Record(t.Context(), RiskToolEvent{
		Tool:           "create_risk_exclusion",
		Outcome:        "conflict",
		Replay:         riskTelemetryMatched,
		CatalogVersion: policycatalog.SchemaV1,
		Reconciliation: riskTelemetryScheduled,
	}, 250*time.Millisecond)
	telemetry.Record(t.Context(), RiskToolEvent{
		Tool:           "sensitive-policy-name",
		Outcome:        "https://untrusted.example/token",
		Replay:         "receipt-secret",
		CatalogVersion: "customer-catalog",
		Reconciliation: "sensitive-value",
	}, time.Second)

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))

	calls := oauthCounterPoints(t, metrics, platformMCPRiskToolCallMetric)
	require.Len(t, calls, 1)
	require.Equal(t, int64(1), calls[0].Value)
	requireRiskMetricAttributes(t, calls[0].Attributes)

	durations := oauthHistogramPoints(t, metrics, platformMCPRiskToolDurationMetric)
	require.Len(t, durations, 1)
	require.Equal(t, uint64(1), durations[0].Count)
	requireRiskMetricAttributes(t, durations[0].Attributes)
}

func TestRiskMutationTelemetryClassifiesTypedResultsAndRefusals(t *testing.T) {
	t.Parallel()

	created := CreateRiskExclusionToolOutput{
		CreateRiskExclusionReceiptResult: CreateRiskExclusionReceiptResult{MatchedExisting: true, Reconciliation: riskTelemetryScheduled},
		Receipt:                          RiskMutationToolReceipt{Replayed: false},
	}
	event := riskMutationSuccessEvent("create_risk_exclusion", created)
	require.Equal(t, riskTelemetryMatched, event.Replay)
	require.Equal(t, riskTelemetryScheduled, event.Reconciliation)

	created.Receipt.Replayed = true
	event = riskMutationSuccessEvent("create_risk_exclusion", created)
	require.Equal(t, riskTelemetryReceiptReplay, event.Replay)

	require.Equal(t, "conflict", riskMutationTelemetryOutcome(&ToolRefusalError{Code: "conflict", Payload: "sensitive prompt"}))
	require.Equal(t, "unavailable", riskMutationTelemetryOutcome(&ToolRefusalError{Code: "", Payload: "sensitive prompt"}))
}

func TestRiskToolWrappersRecordTypedOutcomes(t *testing.T) {
	t.Parallel()

	recorder := &recordingRiskTelemetry{}
	ctx := ContextWithPrincipal(t.Context(), Principal{UserID: "<USER_ID>", OrganizationID: "<ORG_ID>"})
	_, _, err := riskReadToolCall(ctx, recorder, "list_risk_policies", func(Principal) (ListRiskPoliciesOutput, error) {
		return ListRiskPoliciesOutput{}, ErrRiskReadInvalid
	})
	require.NoError(t, err)
	require.Len(t, recorder.events, 1)
	require.Equal(t, "invalid_request", recorder.events[0].Outcome)

	reg := newRegistrar(newTestMCPServer())
	reg.withRiskTelemetry(recorder)
	wrapped := instrumentRiskMutation(reg, "create_risk_exclusion", func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, CreateRiskExclusionToolOutput, error) {
		return nil, CreateRiskExclusionToolOutput{
			CreateRiskExclusionReceiptResult: CreateRiskExclusionReceiptResult{MatchedExisting: false, Reconciliation: riskTelemetryScheduled},
			Receipt:                          RiskMutationToolReceipt{Replayed: true},
		}, nil
	})
	_, _, err = wrapped(ctx, nil, map[string]any{})
	require.NoError(t, err)
	require.Len(t, recorder.events, 2)
	require.Equal(t, "succeeded", recorder.events[1].Outcome)
	require.Equal(t, riskTelemetryReceiptReplay, recorder.events[1].Replay)
	require.Equal(t, riskTelemetryScheduled, recorder.events[1].Reconciliation)
}

func TestRiskTelemetryRejectsUnboundedDimensions(t *testing.T) {
	t.Parallel()

	require.True(t, validRiskToolEvent(riskTelemetryEvent("list_risk_policies", "succeeded")))
	for _, event := range []RiskToolEvent{
		{Tool: "customer-tool", Outcome: "succeeded", Replay: riskTelemetryNotApplicable, CatalogVersion: policycatalog.SchemaV1, Reconciliation: riskTelemetryNotApplicable},
		{Tool: "list_risk_policies", Outcome: "sensitive prompt", Replay: riskTelemetryNotApplicable, CatalogVersion: policycatalog.SchemaV1, Reconciliation: riskTelemetryNotApplicable},
		{Tool: "list_risk_policies", Outcome: "succeeded", Replay: "receipt-id", CatalogVersion: policycatalog.SchemaV1, Reconciliation: riskTelemetryNotApplicable},
		{Tool: "list_risk_policies", Outcome: "succeeded", Replay: riskTelemetryNotApplicable, CatalogVersion: "future-or-user-value", Reconciliation: riskTelemetryNotApplicable},
		{Tool: "list_risk_policies", Outcome: "succeeded", Replay: riskTelemetryNotApplicable, CatalogVersion: policycatalog.SchemaV1, Reconciliation: "customer-state"},
	} {
		require.False(t, validRiskToolEvent(event))
	}
}

func requireRiskMetricAttributes(t *testing.T, attributes attribute.Set) {
	t.Helper()

	want := map[string]string{
		"platform_mcp.risk.tool":            "create_risk_exclusion",
		"platform_mcp.risk.outcome":         "conflict",
		"platform_mcp.risk.replay":          riskTelemetryMatched,
		"platform_mcp.risk.catalog_version": policycatalog.SchemaV1,
		"platform_mcp.risk.reconciliation":  riskTelemetryScheduled,
	}
	got := attributes.ToSlice()
	require.Len(t, got, len(want))
	for _, kv := range got {
		value, ok := want[string(kv.Key)]
		require.True(t, ok, "unexpected metric attribute %q", kv.Key)
		require.Equal(t, value, kv.Value.AsString())
	}
}

var _ RiskTelemetry = (*recordingRiskTelemetry)(nil)

type recordingRiskTelemetry struct {
	events []RiskToolEvent
}

func (t *recordingRiskTelemetry) Record(_ context.Context, event RiskToolEvent, _ time.Duration) {
	t.events = append(t.events, event)
}
