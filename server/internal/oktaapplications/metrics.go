package oktaapplications

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterReconcileOutcome = "gram.okta_applications.reconcile.outcome"

// Run outcomes; a failed run records its typed reason as the outcome. The
// reason strings also appear in the API design enum.
const (
	outcomeSucceeded = "succeeded"

	reasonRateLimited        = "rate_limited"
	reasonCredentialRejected = "credential_rejected" //nolint:gosec // G101 false positive: a reason label.
	reasonOktaUnreachable    = "okta_unreachable"
	reasonClientUnavailable  = "client_unavailable"
	reasonSuperseded         = "superseded"
	reasonInterrupted        = "interrupted"
)

type syncMetrics struct {
	outcome metric.Int64Counter
}

func newSyncMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *syncMetrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oktaapplications")
	outcome, err := meter.Int64Counter(
		meterReconcileOutcome,
		metric.WithDescription("Okta applications reconcile runs by outcome."),
		metric.WithUnit("{run}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterReconcileOutcome), attr.SlogError(err))
	}
	return &syncMetrics{outcome: outcome}
}

func (m *syncMetrics) record(ctx context.Context, outcome string, truncated bool) {
	if m == nil || m.outcome == nil {
		return
	}
	m.outcome.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.Bool("truncated", truncated),
	))
}
