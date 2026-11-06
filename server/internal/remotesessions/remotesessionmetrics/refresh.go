package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterUpstreamRefresh = "gram.remote_session.upstream_refresh"

const meterStoredTokenUnreadable = "gram.remote_session.stored_token_unreadable" //nolint:gosec // G101: a metric name, not a credential

// Refresh holds the upstream-refresh instrument: an unsampled census of
// refresh_token grant attempts against upstream identity providers, one count
// per attempt by its outcome and by which caller triggered it.
//
// This is the steady-state session health signal the flow-level oauth.flow.*
// counters do not cover: a session that connected fine and then silently
// stopped refreshing shows up here and nowhere else.
type Refresh struct {
	attempts metric.Int64Counter

	// unreadable counts stored tokens the current key could not decrypt. A
	// spike means a key problem rather than a few damaged rows.
	unreadable metric.Int64Counter
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

	unreadable, err := meter.Int64Counter(
		meterStoredTokenUnreadable,
		metric.WithDescription("Attempts to use a stored upstream token of a Remote Session that could not be decrypted, by which token and outcome. Counts attempts, not sessions: one unreadable row counts on every use."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStoredTokenUnreadable), attr.SlogError(err))
	}

	return &Refresh{attempts: attempts, unreadable: unreadable}
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

// RecordStoredTokenUnreadable counts one attempt to use a stored token that could not be decrypted.
func (m *Refresh) RecordStoredTokenUnreadable(ctx context.Context, token StoredToken, outcome StoredTokenOutcome) {
	if m == nil || m.unreadable == nil {
		return
	}
	m.unreadable.Add(ctx, 1, metric.WithAttributes(attr.OAuthStoredToken(token), attr.Outcome(outcome)))
}
