package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterUpstreamRevoke = "gram.remote_session.upstream_revoke"

// Revoke holds the upstream-revoke instrument.
type Revoke struct {
	outcome metric.Int64Counter
}

func NewRevoke(logger *slog.Logger, meterProvider metric.MeterProvider) *Revoke {
	meter := meterProvider.Meter(meterScope)

	outcome, err := meter.Int64Counter(
		meterUpstreamRevoke,
		metric.WithDescription("RFC 7009 upstream token revocations attempted when a Remote Session is revoked, by outcome."),
		metric.WithUnit("{revocation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUpstreamRevoke), attr.SlogError(err))
	}

	return &Revoke{outcome: outcome}
}

// Record counts one attempted upstream revocation by its outcome. The issuer
// URL dimension names the actual upstream for platform administrators; empty
// when the revocation died before an issuer was resolved.
func (m *Revoke) Record(ctx context.Context, issuerURL string, outcome RevokeOutcome) {
	if m == nil || m.outcome == nil {
		return
	}
	m.outcome.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthIssuer(issuerURL),
		attr.Outcome(outcome),
	))
}
