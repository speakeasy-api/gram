package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterUpstreamAuthorize = "gram.remote_session.upstream_authorize"

	meterUpstreamAuthorizeUnanswered = "gram.remote_session.upstream_authorize.unanswered"
)

// Authorize holds the upstream-authorize instrument: an unsampled census of
// authorize-URL attempts against upstream identity providers, one count per
// flow the consent screen tries to send a user out on.
//
// Attempts, not completed builds, on purpose: it records at
// BuildAuthorizationUrl entry, before the endpoint validations and the Redis
// write, so a flow that dies there still counts. A policy gate on the
// upstream (such as PKCE enforcement) would evaluate at that same entry
// point, so exit-recording would undercount exactly the flows such a gate
// cares about — and failures past the entry are already visible through the
// flow-level oauth.flow.failed counter and error logs.
type Authorize struct {
	flows metric.Int64Counter

	// unanswered counts authorize legs found still pending when the same
	// user restarted the login: the provider never redirected back to Gram,
	// typically because it rendered its own error page. Only a restart
	// reveals one, so a leg the user abandons outright is not counted.
	unanswered metric.Int64Counter
}

func NewAuthorize(logger *slog.Logger, meterProvider metric.MeterProvider) *Authorize {
	meter := meterProvider.Meter(meterScope)

	flows, err := meter.Int64Counter(
		meterUpstreamAuthorize,
		metric.WithDescription("Upstream authorize URL attempts for Remote Session OAuth flows, by the issuer's advertised PKCE support."),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUpstreamAuthorize), attr.SlogError(err))
	}

	unanswered, err := meter.Int64Counter(
		meterUpstreamAuthorizeUnanswered,
		metric.WithDescription("Upstream authorize legs that never redirected back to Gram before the user restarted the login."),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUpstreamAuthorizeUnanswered), attr.SlogError(err))
	}

	return &Authorize{flows: flows, unanswered: unanswered}
}

// Record counts one authorize-URL attempt.
func (m *Authorize) Record(ctx context.Context, issuerURL string, pkceSupport PKCESupportState) {
	if m == nil || m.flows == nil {
		return
	}
	m.flows.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthIssuer(issuerURL),
		attr.PKCESupport(pkceSupport),
	))
}

// RecordUnanswered counts one authorize leg the issuer never answered. The
// issuer is the only dimension: the leg belongs to a user, and no user
// identifier goes on a metric.
func (m *Authorize) RecordUnanswered(ctx context.Context, issuerURL string) {
	if m == nil || m.unanswered == nil {
		return
	}
	m.unanswered.Add(ctx, 1, metric.WithAttributes(attr.OAuthIssuer(issuerURL)))
}
