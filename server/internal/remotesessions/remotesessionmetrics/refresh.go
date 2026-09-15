package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterUpstreamRefresh = "gram.remote_session.upstream_refresh"

// Refresh holds the upstream-refresh instrument: an unsampled census of
// refresh_token grant attempts against upstream identity providers, one count
// per attempt by its outcome and by which caller triggered it.
//
// This is the steady-state session health signal the flow-level oauth.flow.*
// counters do not cover: a session that connected fine and then silently
// stopped refreshing shows up here and nowhere else.
type Refresh struct {
	attempts metric.Int64Counter
}

func NewRefresh(logger *slog.Logger, meterProvider metric.MeterProvider) *Refresh {
	meter := meterProvider.Meter(meterScope)

	attempts, err := meter.Int64Counter(
		meterUpstreamRefresh,
		metric.WithDescription("Upstream refresh_token grant attempts for Remote Sessions, by outcome and trigger. Counts attempts, not POSTs: concurrent callers single-flight and the losers record adopted_concurrent_winner."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUpstreamRefresh), attr.SlogError(err))
	}

	return &Refresh{attempts: attempts}
}

// Record counts one refresh attempt by its outcome and trigger. The issuer
// URL dimension names the actual upstream for platform administrators; empty
// when the attempt died before the session's client and issuer rows could be
// loaded.
func (m *Refresh) Record(ctx context.Context, issuerURL string, trigger RefreshTrigger, outcome RefreshOutcome) {
	if m == nil || m.attempts == nil {
		return
	}
	m.attempts.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthIssuer(issuerURL),
		attr.OAuthRefreshTrigger(trigger),
		attr.Outcome(outcome),
	))
}
