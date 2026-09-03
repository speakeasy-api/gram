package mcpmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// InstrumentToolsetSlugFallback counts requests a public MCP surface served
// through the legacy toolsets.mcp_slug lookup after an mcp_endpoints address
// miss. Recorded only when the legacy lookup actually resolved a live toolset,
// so the series converges to zero once every hosted server has its wrapper and
// endpoints — this counter reading zero for the agreed window is the merge
// gate for removing the fallback (AIS-646).
const InstrumentToolsetSlugFallback = "mcp.toolset_slug_fallback"

// InstrumentLegacyAudienceAccepted counts bearer validations on a
// toolset-backed wrapper that only passed against the legacy toolset-URN
// audience. Same merge-gate role as InstrumentToolsetSlugFallback: zero for
// longer than the maximum session lifetime observed at backfill time lets
// AIS-646 delete the acceptance.
const InstrumentLegacyAudienceAccepted = "mcp.legacy_audience_accepted"

// LegacyFallbackEntryPoint is the closed set of public surfaces that can
// resolve a request through the legacy toolsets.mcp_slug lookup. It is the
// only dimension on the fallback counter; keep it bounded — the counter's
// purpose is a per-surface zero check, not per-server attribution.
type LegacyFallbackEntryPoint string

const (
	// LegacyFallbackServePublic: POST /mcp/{slug} runtime dispatch.
	LegacyFallbackServePublic LegacyFallbackEntryPoint = "serve_public"
	// LegacyFallbackProxyGetDelete: the GET (SSE) / DELETE /mcp/{slug}
	// handlers, whose legacy outcome after an address miss still answers for a
	// live toolset slug (405 today, 404 once the fallback is removed).
	LegacyFallbackProxyGetDelete LegacyFallbackEntryPoint = "proxy_get_delete"
	// LegacyFallbackWellKnownProtectedResource: RFC 9728 metadata route.
	LegacyFallbackWellKnownProtectedResource LegacyFallbackEntryPoint = "well_known_protected_resource"
	// LegacyFallbackWellKnownAuthorizationServer: RFC 8414 metadata route.
	LegacyFallbackWellKnownAuthorizationServer LegacyFallbackEntryPoint = "well_known_authorization_server"
	// LegacyFallbackOAuth: the issuer-gated OAuth handler family resolving via
	// LoadResolvedMcpEndpointBySlug (authorize, token, register, revoke,
	// consent — on both the /mcp and /x/mcp surfaces).
	LegacyFallbackOAuth LegacyFallbackEntryPoint = "oauth"
	// LegacyFallbackInstallPage: the install-page resolver in mcpmetadata.
	LegacyFallbackInstallPage LegacyFallbackEntryPoint = "install_page"
	// LegacyFallbackChallengeResume: cached authorization-challenge state
	// resuming through the strict-platform toolset lookup.
	LegacyFallbackChallengeResume LegacyFallbackEntryPoint = "challenge_resume"
)

// LegacyFallbackCounter owns the two migration merge-gate instruments for the
// toolsets → mcp_servers cutover (AIS-633). A nil *LegacyFallbackCounter is
// valid — every Record becomes a no-op — so callers never nil-check.
type LegacyFallbackCounter struct {
	slugFallback     metric.Int64Counter
	audienceAccepted metric.Int64Counter
}

// NewLegacyFallbackCounter constructs both instruments. An instrument-creation
// failure is logged and leaves that instrument nil; Record methods handle nil
// instruments so partial construction still records what it can.
func NewLegacyFallbackCounter(meter metric.Meter, logger *slog.Logger) *LegacyFallbackCounter {
	slugFallback, err := meter.Int64Counter(
		InstrumentToolsetSlugFallback,
		metric.WithDescription("MCP requests resolved through the legacy toolsets.mcp_slug lookup after an mcp_endpoints miss, by entry point"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(InstrumentToolsetSlugFallback), attr.SlogError(err))
	}

	audienceAccepted, err := meter.Int64Counter(
		InstrumentLegacyAudienceAccepted,
		metric.WithDescription("Bearer validations on toolset-backed wrappers that passed only against the legacy toolset-URN audience, by issuer"),
		metric.WithUnit("{validation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(InstrumentLegacyAudienceAccepted), attr.SlogError(err))
	}

	return &LegacyFallbackCounter{slugFallback: slugFallback, audienceAccepted: audienceAccepted}
}

// RecordToolsetSlugFallback counts one request served through the legacy
// toolsets.mcp_slug lookup. Call it only after the legacy lookup resolved a
// live toolset — counting bare address misses would keep the series nonzero
// forever on scanner probes of nonexistent slugs.
func (c *LegacyFallbackCounter) RecordToolsetSlugFallback(ctx context.Context, entryPoint LegacyFallbackEntryPoint) {
	if c == nil || c.slugFallback == nil {
		return
	}
	c.slugFallback.Add(ctx, 1, metric.WithAttributes(attr.McpEntryPoint(entryPoint)))
}

// RecordLegacyAudienceAccepted counts one bearer accepted via the legacy
// toolset-URN audience, dimensioned by the issuer whose sessions still carry
// it.
func (c *LegacyFallbackCounter) RecordLegacyAudienceAccepted(ctx context.Context, issuerID string) {
	if c == nil || c.audienceAccepted == nil {
		return
	}
	c.audienceAccepted.Add(ctx, 1, metric.WithAttributes(attr.UserSessionIssuerID(issuerID)))
}
