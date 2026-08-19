package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterUpstreamAuthorize = "gram.remote_session.upstream_authorize"

// Authorize holds the upstream-authorize instrument: an unsampled census of
// authorize-URL builds against upstream identity providers, one count per
// flow the consent screen sends a user out on.
//
// It records at BuildAuthorizationUrl entry, before the endpoint validations
// and the Redis write, so flows that die on unrelated errors still count
// toward the census.
type Authorize struct {
	flows metric.Int64Counter
}

func NewAuthorize(logger *slog.Logger, meterProvider metric.MeterProvider) *Authorize {
	meter := meterProvider.Meter(meterScope)

	flows, err := meter.Int64Counter(
		meterUpstreamAuthorize,
		metric.WithDescription("Upstream authorize URLs built for Remote Session OAuth flows, by the issuer's advertised PKCE support."),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUpstreamAuthorize), attr.SlogError(err))
	}

	return &Authorize{flows: flows}
}

// Record counts one authorize-URL build.
func (m *Authorize) Record(ctx context.Context, issuerURL string, pkceSupport PKCESupportState) {
	if m == nil || m.flows == nil {
		return
	}
	m.flows.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthIssuer(issuerURL),
		attr.PKCESupport(pkceSupport),
	))
}
