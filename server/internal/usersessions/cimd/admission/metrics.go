package admission

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// meterAdmissionDecisions is deliberately its own instrument rather than a
// new result label on cimd.fetch.attempts. That counter documents an
// invariant — exactly one point per resolve attempt — and it is named for
// fetches. Admission denials happen when no fetch runs at all, so folding
// them in would silently change the denominator of every existing
// fetch-success chart.
const meterAdmissionDecisions = "cimd.admission.decisions"

// Metrics records CIMD admission decisions. Safe for concurrent use; one
// instance should live for the process lifetime so the instrument is
// created once.
type Metrics struct {
	decisions metric.Int64Counter
}

func NewMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *Metrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission")

	decisions, err := meter.Int64Counter(
		meterAdmissionDecisions,
		metric.WithDescription("Count of CIMD admission decisions by effective issuer mode and outcome"),
		metric.WithUnit("{decision}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterAdmissionDecisions), attr.SlogError(err))
	}

	return &Metrics{decisions: decisions}
}

// RecordAdmitted counts a client_id permitted by the issuer's policy.
//
// The reason, not a bare "admitted", is what makes the counter useful after
// rollout: it separates catalog hits from an issuer's own custom URLs, and
// exact catalog entries from wildcard ones.
func (m *Metrics) RecordAdmitted(ctx context.Context, mode Mode, reason AdmitReason) {
	m.record(ctx, mode, string(reason))
}

// RecordDenied counts a client_id rejected by the issuer's policy.
//
// Both dimensions are low-cardinality and operator-controlled. The
// presented client_id is deliberately absent: it is attacker-chosen and
// unbounded on this surface, and a denial is cheap enough to generate at
// volume. It belongs on the denial log line instead, which is where an
// operator diagnosing a preset miss will look for it.
func (m *Metrics) RecordDenied(ctx context.Context, mode Mode, reason DenialReason) {
	m.record(ctx, mode, string(reason))
}

func (m *Metrics) record(ctx context.Context, mode Mode, outcome string) {
	if m == nil || m.decisions == nil {
		return
	}
	m.decisions.Add(ctx, 1, metric.WithAttributes([]attribute.KeyValue{
		attr.CIMDAdmissionMode(mode),
		attr.CIMDAdmissionOutcome(outcome),
	}...))
}
