package remotesessionmetrics

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
)

const (
	meterScopeResolution = "gram.remote_session.scope_resolution"
	meterResourceProbe   = "gram.remote_session.resource_probe.duration"
)

// ScopeSource names the precedence step that produced a login's scope request.
type ScopeSource string

const (
	// ScopeSourceResourcePin: the operator's pin on the protected resource.
	ScopeSourceResourcePin ScopeSource = "resource_pin"

	// ScopeSourceClientScope: the client's stored scope.
	ScopeSourceClientScope ScopeSource = "client_scope"

	// ScopeSourceChallengeScope: the resource's last WWW-Authenticate scope param.
	ScopeSourceChallengeScope ScopeSource = "challenge_scope"

	// ScopeSourceLiveResource: the resource's scopes_supported read during this login.
	ScopeSourceLiveResource ScopeSource = "live_resource"

	// ScopeSourceCachedResource: the resource's scopes_supported from an earlier read.
	ScopeSourceCachedResource ScopeSource = "cached_resource"

	// ScopeSourceIssuerOverride: the operator's override on the issuer, sent verbatim.
	ScopeSourceIssuerOverride ScopeSource = "issuer_override"

	// ScopeSourceIssuerCatalogue: the issuer's whole scopes_supported.
	ScopeSourceIssuerCatalogue ScopeSource = "issuer_catalogue"

	// ScopeSourceNone: nothing named a scope, so none is sent.
	ScopeSourceNone ScopeSource = "none"
)

// ScopeResolution counts how logins decide their scope request, by the
// precedence step that won and how the resource's metadata was resolved,
// and times the live probes that ran.
type ScopeResolution struct {
	resolutions   metric.Int64Counter
	probeDuration metric.Float64Histogram
}

func NewScopeResolution(logger *slog.Logger, meterProvider metric.MeterProvider) *ScopeResolution {
	meter := meterProvider.Meter(meterScope)

	resolutions, err := meter.Int64Counter(
		meterScopeResolution,
		metric.WithDescription("Remote Session login scope requests, by the precedence step that produced them and the resource metadata probe outcome."),
		metric.WithUnit("{login}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterScopeResolution), attr.SlogError(err))
	}

	probeDuration, err := meter.Float64Histogram(
		meterResourceProbe,
		metric.WithDescription("Time a Remote Session login spent probing its protected resource's metadata, by probe outcome."),
		metric.WithUnit("s"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterResourceProbe), attr.SlogError(err))
	}

	return &ScopeResolution{resolutions: resolutions, probeDuration: probeDuration}
}

// RecordProbe times one live probe a login waited on.
func (m *ScopeResolution) RecordProbe(ctx context.Context, outcome protectedresource.ProbeOutcome, duration time.Duration) {
	if m == nil || m.probeDuration == nil {
		return
	}
	m.probeDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attr.OAuthResourceProbeOutcome(outcome)))
}

// Record counts one login's scope resolution.
func (m *ScopeResolution) Record(ctx context.Context, source ScopeSource, outcome protectedresource.ProbeOutcome) {
	if m == nil || m.resolutions == nil {
		return
	}
	m.resolutions.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthScopeSource(source),
		attr.OAuthResourceProbeOutcome(outcome),
	))
}
