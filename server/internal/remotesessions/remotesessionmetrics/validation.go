package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterValidation = "gram.remote_session.validation"

// Validation counts live-validation probes by outcome.
type Validation struct {
	probes metric.Int64Counter
}

func NewValidation(logger *slog.Logger, meterProvider metric.MeterProvider) *Validation {
	meter := meterProvider.Meter(meterScope)

	probes, err := meter.Int64Counter(
		meterValidation,
		metric.WithDescription("Live validation probes of stored Remote Session credentials against their upstream MCP server, by outcome and issuer URL."),
		metric.WithUnit("{probe}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterValidation), attr.SlogError(err))
	}

	return &Validation{probes: probes}
}

// Record counts one probe by outcome (a remotesessions.ValidationOutcome) and issuer URL.
func (m *Validation) Record(ctx context.Context, issuerURL string, outcome string) {
	if m == nil || m.probes == nil {
		return
	}
	m.probes.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthIssuer(issuerURL),
		attr.Outcome(outcome),
	))
}
