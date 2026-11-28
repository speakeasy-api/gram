// Package riskhealthtest reads risk.analysis.evaluations in tests of the
// engines and policy layers that record it.
package riskhealthtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"
)

// Sample identifies one risk.analysis.evaluations data point.
type Sample struct {
	// OrgID is the gram.org.id tag.
	OrgID string

	// Component is the gram.risk.analysis.component tag.
	Component riskhealth.Component

	// Outcome is the gram.outcome tag.
	Outcome riskhealth.Outcome

	// Reason is the gram.risk.analysis.degradation_reason tag.
	Reason riskhealth.Reason

	// PolicyID is the gram.risk.policy_id tag, empty when the sample has none.
	PolicyID string
}

// Meter is a meter provider backed by a manual reader.
type Meter struct {
	// Provider is the meter provider to hand to the code under test.
	Provider *sdkmetric.MeterProvider

	reader *sdkmetric.ManualReader
}

// NewMeter returns a Meter shut down when the test ends.
func NewMeter(t *testing.T) *Meter {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	return &Meter{Provider: provider, reader: reader}
}

// Evaluations collects risk.analysis.evaluations, counted per sample.
func (m *Meter) Evaluations(t *testing.T) map[Sample]int64 {
	t.Helper()

	var metrics metricdata.ResourceMetrics
	require.NoError(t, m.reader.Collect(t.Context(), &metrics))

	counts := make(map[Sample]int64)
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != riskhealth.MeterEvaluations {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok, "risk.analysis.evaluations is an int64 counter")
			for _, point := range sum.DataPoints {
				org, _ := point.Attributes.Value(attr.OrganizationIDKey)
				component, _ := point.Attributes.Value(attr.RiskAnalysisComponentKey)
				outcome, _ := point.Attributes.Value(attr.OutcomeKey)
				reason, _ := point.Attributes.Value(attr.RiskAnalysisDegradationReasonKey)
				policyID, _ := point.Attributes.Value(attr.RiskPolicyIDKey)
				counts[Sample{
					OrgID:     org.AsString(),
					Component: riskhealth.Component(component.AsString()),
					Outcome:   riskhealth.Outcome(outcome.AsString()),
					Reason:    riskhealth.Reason(reason.AsString()),
					PolicyID:  policyID.AsString(),
				}] += point.Value
			}
		}
	}
	return counts
}
