package risk_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	pubsubDegradedLogMessage     = "pub/sub enforcement lane degraded"
	pubsubCallerBudgetLogMessage = "pub/sub enforcement lane cut short by caller budget"
)

func TestScanner_PubsubCallerCancellationDoesNotDegrade(t *testing.T) {
	t.Parallel()

	logs, metrics := runPubsubLaneFailure(t, fmt.Errorf("request typed enforcement lane: %w", context.Canceled), false)

	require.NotContains(t, logs, pubsubDegradedLogMessage)
	require.Empty(t, pubsubDegradedCounts(t, metrics))
}

func TestScanner_PubsubDeadlineDoesDegrade(t *testing.T) {
	t.Parallel()

	logs, metrics := runPubsubLaneFailure(t, fmt.Errorf("request typed enforcement lane: %w", context.DeadlineExceeded), false)

	require.Contains(t, logs, pubsubDegradedLogMessage)
	require.Equal(t, map[string]int64{"deadline": 1}, pubsubDegradedCounts(t, metrics))
}

// A lane the caller's own deadline cut short never had its full wait, so it
// counts under its own reason and stays off the error level the consumer
// failures use.
func TestScanner_PubsubCallerBudgetDegradesUnderItsOwnReason(t *testing.T) {
	t.Parallel()

	logs, metrics := runPubsubLaneFailure(t, fmt.Errorf("request typed enforcement lane: %w", context.DeadlineExceeded), true)

	require.Contains(t, logs, pubsubCallerBudgetLogMessage)
	require.NotContains(t, logs, pubsubDegradedLogMessage)
	require.NotContains(t, logs, `"level":"ERROR"`)
	require.Equal(t, map[string]int64{"budget_exhausted": 1}, pubsubDegradedCounts(t, metrics))
}

func runPubsubLaneFailure(t *testing.T, laneErr error, callerBudget bool) (string, metricdata.ResourceMetrics) {
	t.Helper()

	ctx, ti := newTestRiskService(t)
	insertPresidioBlockPolicy(t, ti, ctx, "remote", []string{"EMAIL_ADDRESS"})
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	dispatcher := &fakeEnforcementDispatcher{fn: func(request enforcereply.DispatchRequest) (enforcereply.Outcome, error) {
		lane := request.Lanes[0]
		budget := map[enforcereply.Lane]bool{}
		if callerBudget {
			budget[lane] = true
		}
		return enforcereply.Outcome{
			ByLane:       map[enforcereply.Lane]*riskv1.EnforcementReply{},
			Failed:       map[enforcereply.Lane]error{lane: laneErr},
			Complete:     false,
			Deadline:     false,
			CallerBudget: budget,
			Truncated:    false,
		}, nil
	}}
	scanner, err := risk.NewScannerWithEnforcementDispatcher(
		logger,
		testenv.NewTracerProvider(t),
		meterProvider,
		ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn),
		nil,
		nil,
		nil,
		pubsubEnforcementFlags(ctx),
		testCELEngine(t),
		dispatcher,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)
	require.NoError(t, err)

	result, err := scanner.ScanForEnforcement(ctx, realtimeScanRequest(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "alice@example.com", message.User, ""))
	require.NoError(t, err)
	require.Nil(t, result)

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &metrics))
	return logs.String(), metrics
}

func pubsubDegradedCounts(t *testing.T, metrics metricdata.ResourceMetrics) map[string]int64 {
	t.Helper()

	counts := make(map[string]int64)
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != "risk.enforcement.pubsub_degraded" {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				reason, ok := point.Attributes.Value(attr.ReasonKey)
				require.True(t, ok)
				counts[reason.AsString()] += point.Value
			}
		}
	}
	return counts
}
