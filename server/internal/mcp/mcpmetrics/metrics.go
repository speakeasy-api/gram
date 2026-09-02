package mcpmetrics

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// InstrumentMCPRequestRejected is the OTel instrument name for the counter of
// requests the Session OAuth authentication gate rejected before dispatch.
const InstrumentMCPRequestRejected = "mcp.request.rejected"

// OAuthFlowStage is the closed set of coarse stages at which a user-facing
// OAuth flow can terminally resolve to a non-completion outcome (failed or
// declined). It names the handler leg where the flow ended. Kept as a bounded
// enum — and deliberately decoupled from the free-form reason strings the
// handlers log — so the `gram.oauth.flow_stage` metric dimension stays
// low-cardinality. Add a value here only when a new terminal point is
// instrumented.
type OAuthFlowStage string

const (
	// OAuthFlowStageAuthorize: the flow died in /authorize after the challenge
	// was minted (e.g. building the IDP authorization URL failed, which
	// surfaces a misconfigured IDP).
	OAuthFlowStageAuthorize OAuthFlowStage = "authorize"
	// OAuthFlowStageIDPCallback: the flow ended on the private-toolset IDP
	// return leg (HandleIDPCallback) — an IDP error, a failed code exchange,
	// an org-membership denial, or the user cancelling at the IDP.
	OAuthFlowStageIDPCallback OAuthFlowStage = "idp_callback"
	// OAuthFlowStageConsent: the flow ended at the consent step (HandleConsent
	// POST) — the user declined, or the approval could not be persisted.
	OAuthFlowStageConsent OAuthFlowStage = "consent"
	// OAuthFlowStageToken: the authorization_code token exchange was rejected
	// (HandleToken). Refresh-token grants are NOT part of a flow and never
	// record here.
	OAuthFlowStageToken OAuthFlowStage = "token"
)

// Metrics is the mcp service's full instrument set. A nil *Metrics is valid —
// every Record method becomes a no-op — and each method is also
// nil-instrument-safe, so a partially constructed value still records what
// it can.
type Metrics struct {
	// mcpInitializeCounter is the unsampled census of observed handshakes by
	// protocol revision. A counter rather than a span attribute because traces
	// are sampled and so cannot be counted, and separate from
	// mcpRequestDuration because that histogram carries a per-server URL
	// dimension this must not be multiplied by.
	mcpInitializeCounter metric.Int64Counter

	// requestCensus is the unsampled per-request census (mcp.request) emitted
	// at the JSON-RPC dispatch sites for the hosted and platform surfaces. The
	// remote/tunnel /x/mcp backends publish the same instrument from the
	// proxy's request interceptor, which constructs its own [RequestCounter].
	requestCensus *RequestCounter

	// metaMemberDispatchCounter counts proxied meta member dials by backend
	// kind and credential routing outcome. No per-gateway dimension: that
	// view lives in ClickHouse.
	metaMemberDispatchCounter metric.Int64Counter

	// mcpRequestRejectedCounter is the unsampled census of requests the issuer
	// gate turned away before dispatch: the population that never reaches
	// requestCensus. It carries a per-server URL because "which server is
	// rejecting" is the operational question; the URL is rebuilt from the
	// resolved endpoint rather than taken from the request so an
	// unauthenticated caller cannot mint series through the query string.
	mcpRequestRejectedCounter metric.Int64Counter

	mcpToolCallCounter metric.Int64Counter
	mcpRequestDuration metric.Float64Histogram
	identityCoverage   *IdentityCoverageCounter
	legacyFallback     *LegacyFallbackCounter

	// oauthFlow{Started,Completed,Failed,Declined}Counter instrument the
	// user-facing OAuth flow as a unit. They decompose a flow's terminal
	// outcome by intent:
	//   - started:   /authorize minted a challenge (fires once per flow).
	//   - completed: a token was issued (authorization_code grant succeeded).
	//   - failed:    the user wanted in but config/code/policy refused or
	//     errored (grant/PKCE/redirect mismatch, IDP error, org-membership
	//     denial, internal 5xx) — the alertable bucket, tagged by stage.
	//   - declined:  the user reached a decision point and chose "no" (consent
	//     deny, IDP access_denied) — the machinery worked; not alertable.
	// The remainder (started - completed - failed - declined) is silent
	// abandonment (user vanished mid-flow), observable only as a ratio gap.
	// The companion Datadog monitor can therefore alert on a clean signal
	// (failed/started, or completed/(completed+failed)) instead of per-URL
	// status.
	oauthFlowStartedCounter   metric.Int64Counter
	oauthFlowCompletedCounter metric.Int64Counter
	oauthFlowFailedCounter    metric.Int64Counter
	oauthFlowDeclinedCounter  metric.Int64Counter

	// oauthRefreshTokenReplayServedCounter counts refresh responses served from
	// the encrypted replay cache rather than by rotating the database session.
	oauthRefreshTokenReplayServedCounter metric.Int64Counter
}

// NewMetrics constructs every instrument the mcp service publishes. Each
// instrument creation failure is logged and leaves that instrument nil; the
// Record methods handle nil instruments so partial construction still
// produces a usable value.
func NewMetrics(meter metric.Meter, logger *slog.Logger) *Metrics {
	mcpToolCallCounter, err := meter.Int64Counter(
		"mcp.tool.call",
		metric.WithDescription("MCP tool call"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp tool call counter", attr.SlogError(err))
	}

	mcpRequestDuration, err := meter.Float64Histogram(
		"mcp.request.duration",
		metric.WithDescription("Duration of mcp request in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 240),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp request duration", attr.SlogError(err))
	}

	metaMemberDispatchCounter, err := meter.Int64Counter(
		"mcp.meta.member.dispatch",
		metric.WithDescription("Proxied meta MCP member dispatch attempts by backend kind and credential routing outcome"),
		metric.WithUnit("{dispatch}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create meta member dispatch counter", attr.SlogError(err))
	}

	mcpInitializeCounter, err := meter.Int64Counter(
		"mcp.initialize",
		metric.WithDescription("MCP handshakes observed, by requested and negotiated protocol revision"),
		metric.WithUnit("{handshake}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp initialize counter", attr.SlogError(err))
	}

	oauthFlowStartedCounter, err := meter.Int64Counter(
		"oauth.flow.started",
		metric.WithDescription("User-facing OAuth flow initiated at /authorize"),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create oauth flow started counter", attr.SlogError(err))
	}

	oauthFlowCompletedCounter, err := meter.Int64Counter(
		"oauth.flow.completed",
		metric.WithDescription("User-facing OAuth flow completed via successful authorization_code token exchange"),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create oauth flow completed counter", attr.SlogError(err))
	}

	oauthFlowFailedCounter, err := meter.Int64Counter(
		"oauth.flow.failed",
		metric.WithDescription("User-facing OAuth flow terminally failed after a challenge was minted (config/code/policy)"),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create oauth flow failed counter", attr.SlogError(err))
	}

	oauthFlowDeclinedCounter, err := meter.Int64Counter(
		"oauth.flow.declined",
		metric.WithDescription("User-facing OAuth flow declined by the user at a decision point (consent deny, IDP access_denied)"),
		metric.WithUnit("{flow}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create oauth flow declined counter", attr.SlogError(err))
	}

	oauthRefreshTokenReplayServedCounter, err := meter.Int64Counter(
		"oauth.refresh_token.replay.served",
		metric.WithDescription("OAuth refresh-token responses served from the replay cache"),
		metric.WithUnit("{response}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create oauth refresh token replay served counter", attr.SlogError(err))
	}

	mcpRequestRejectedCounter, err := meter.Int64Counter(
		InstrumentMCPRequestRejected,
		metric.WithDescription("MCP requests rejected by the Session OAuth authentication gate before dispatch, by failure reason, server URL, and surface"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(InstrumentMCPRequestRejected), attr.SlogError(err))
	}

	return &Metrics{
		mcpToolCallCounter:                   mcpToolCallCounter,
		mcpRequestDuration:                   mcpRequestDuration,
		mcpInitializeCounter:                 mcpInitializeCounter,
		metaMemberDispatchCounter:            metaMemberDispatchCounter,
		mcpRequestRejectedCounter:            mcpRequestRejectedCounter,
		requestCensus:                        NewRequestCounter(meter, logger),
		identityCoverage:                     NewIdentityCoverageCounter(meter, logger),
		legacyFallback:                       NewLegacyFallbackCounter(meter, logger),
		oauthFlowStartedCounter:              oauthFlowStartedCounter,
		oauthFlowCompletedCounter:            oauthFlowCompletedCounter,
		oauthFlowFailedCounter:               oauthFlowFailedCounter,
		oauthFlowDeclinedCounter:             oauthFlowDeclinedCounter,
		oauthRefreshTokenReplayServedCounter: oauthRefreshTokenReplayServedCounter,
	}
}

func (m *Metrics) RecordMCPToolCall(ctx context.Context, orgID string, mcpURL string, toolName string) {
	if m == nil || m.mcpToolCallCounter == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.McpURL(mcpURL),
		attr.ToolName(toolName),
		attr.OrganizationID(orgID),
	}
	m.mcpToolCallCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

// RecordKillswitchIdentityCoverage records the bounded coverage classes for
// one tools/call observed at a registered MCP checkpoint.
func (m *Metrics) RecordKillswitchIdentityCoverage(ctx context.Context, surface KillswitchCoverageSurface, identity KillswitchIdentityClass, resource KillswitchResourceClass) {
	if m == nil {
		return
	}
	m.identityCoverage.Record(ctx, surface, identity, resource)
}

// RecordToolsetSlugFallback counts one request served through the legacy
// toolsets.mcp_slug lookup after an mcp_endpoints address miss. Semantics on
// [LegacyFallbackCounter.RecordToolsetSlugFallback].
func (m *Metrics) RecordToolsetSlugFallback(ctx context.Context, entryPoint LegacyFallbackEntryPoint) {
	if m == nil {
		return
	}
	m.legacyFallback.RecordToolsetSlugFallback(ctx, entryPoint)
}

// RecordLegacyAudienceAccepted counts one bearer accepted via the legacy
// toolset-URN audience. Semantics on
// [LegacyFallbackCounter.RecordLegacyAudienceAccepted].
func (m *Metrics) RecordLegacyAudienceAccepted(ctx context.Context, issuerID string) {
	if m == nil {
		return
	}
	m.legacyFallback.RecordLegacyAudienceAccepted(ctx, issuerID)
}

// RecordMCPInitialize counts one observed MCP handshake, dimensioned by the
// protocol revision the client requested and the one it was answered with.
//
// This is deliberately a counter and deliberately carries no per-server
// dimension. Traces are sampled at a low fixed rate service-wide, so span
// attributes answer "what version was this failing request?" but cannot answer
// "how many clients are still on 2024-11-05?" — which is the census question
// protocol-version gating decisions depend on. Omitting gram.mcp.url keeps this
// instrument aggregatable across the whole fleet and keeps its series count
// bounded to the version cross-product.
//
// Both dimensions are clamped to the known revision set so a hostile or broken
// client cannot mint unbounded series; the unclamped values remain on the span.
// Both are always recorded, so a breakdown by version accounts for every
// handshake — including clients that named an unknown revision or none at all,
// which are the two cohorts most likely to break under a version ceiling.
func (m *Metrics) RecordMCPInitialize(ctx context.Context, requested, negotiated string) {
	if m == nil || m.mcpInitializeCounter == nil {
		return
	}

	m.mcpInitializeCounter.Add(ctx, 1, metric.WithAttributes(
		attr.MCPRequestedProtocolVersion(mcpversions.Clamp(mcpversions.Sanitize(requested))),
		attr.MCPNegotiatedProtocolVersion(mcpversions.Clamp(mcpversions.Sanitize(negotiated))),
	))
}

// RecordMCPRequest counts one dispatched MCP request on the per-request
// census, dimensioned by clamped protocol revision, clamped method, and
// surface. The census semantics — what counts, what the version dimension
// means, and how this relates to `mcp.initialize` and `mcp.request.duration` —
// are documented on [RequestCounter.Record], which the remote proxy's
// interceptor invokes directly for the /x/mcp traffic that never reaches the
// mcp dispatch.
func (m *Metrics) RecordMCPRequest(ctx context.Context, protocolVersion, method string, surface Surface) {
	if m == nil {
		return
	}

	m.requestCensus.Record(ctx, protocolVersion, method, surface)
}

// RecordMCPRequestRejected counts one MCP request the Session OAuth
// authentication gate turned away before dispatch. reason is the closed set
// the gate logs under gram.oauth.failure_reason; mcpURL is the same
// gram.mcp.url key `mcp.request.duration` and `mcp.tool.call` carry, so every
// per-server MCP metric groups the same way; surface is the same value the
// `mcp.request` census carries, so `rejected / (rejected + request)` is well
// defined per surface.
//
// Rejected requests are absent from every other instrument: the census and
// the duration histogram record after the gate has passed a request. This
// counter therefore partitions traffic at the gate rather than overlapping
// with them.
func (m *Metrics) RecordMCPRequestRejected(ctx context.Context, reason string, mcpURL string, surface Surface) {
	if m == nil || m.mcpRequestRejectedCounter == nil {
		return
	}

	m.mcpRequestRejectedCounter.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthFailureReason(reason),
		attr.McpURL(mcpURL),
		attr.McpSurface(string(surface)),
	))
}

// RecordMCPRequestDuration records one dispatched request's duration. The
// method label is clamped to the known method set: it is client-supplied
// JSON-RPC input, and unclamped it would let a client mint unbounded series
// against a histogram that already carries a per-server URL dimension.
func (m *Metrics) RecordMCPRequestDuration(ctx context.Context, mcpMethod string, mcpURL string, duration time.Duration) {
	if m == nil || m.mcpRequestDuration == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.McpMethod(mcprequests.ClampMethod(mcpMethod)),
		attr.McpURL(mcpURL),
	}

	m.mcpRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(kv...))
}

// oauthFlowDimensions are the low-cardinality attributes shared by every
// OAuth flow counter — enough to group by OAuth configuration without tagging
// per-flow / per-user / per-client values (those belong in logs, not metrics).
func oauthFlowDimensions(issuerID, mcpSlug string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attr.UserSessionIssuerID(issuerID),
		attr.ToolsetMCPSlug(mcpSlug),
	}
}

// RecordOAuthFlowStarted records that a user-facing OAuth flow was initiated
// — emitted once per minted challenge at /authorize.
func (m *Metrics) RecordOAuthFlowStarted(ctx context.Context, issuerID, mcpSlug string) {
	if m == nil || m.oauthFlowStartedCounter == nil {
		return
	}
	m.oauthFlowStartedCounter.Add(ctx, 1, metric.WithAttributes(oauthFlowDimensions(issuerID, mcpSlug)...))
}

// RecordOAuthFlowCompleted records that a user-facing OAuth flow resolved
// successfully — emitted when the authorization_code token exchange succeeds.
func (m *Metrics) RecordOAuthFlowCompleted(ctx context.Context, issuerID, mcpSlug string) {
	if m == nil || m.oauthFlowCompletedCounter == nil {
		return
	}
	m.oauthFlowCompletedCounter.Add(ctx, 1, metric.WithAttributes(oauthFlowDimensions(issuerID, mcpSlug)...))
}

// RecordOAuthFlowFailed records that a user-facing OAuth flow terminally
// failed after a challenge was minted — the user wanted in but config, code,
// or policy refused or errored — tagged with the coarse stage where it died.
// Not emitted for pre-mint /authorize rejections (no started counted), for
// deliberate user declines (see RecordOAuthFlowDeclined), or for refresh_token
// grants (not part of a flow).
func (m *Metrics) RecordOAuthFlowFailed(ctx context.Context, issuerID, mcpSlug string, stage OAuthFlowStage) {
	if m == nil || m.oauthFlowFailedCounter == nil {
		return
	}
	kv := append(oauthFlowDimensions(issuerID, mcpSlug), attr.OAuthFlowStage(string(stage)))
	m.oauthFlowFailedCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

// RecordOAuthFlowDeclined records that a user-facing OAuth flow ended because
// the user deliberately declined at a decision point (consent deny, IDP
// access_denied), tagged with the coarse stage. The machinery worked; this is
// a user choice, not an errant config — kept separate from failed so the
// alertable failure signal stays clean.
func (m *Metrics) RecordOAuthFlowDeclined(ctx context.Context, issuerID, mcpSlug string, stage OAuthFlowStage) {
	if m == nil || m.oauthFlowDeclinedCounter == nil {
		return
	}
	kv := append(oauthFlowDimensions(issuerID, mcpSlug), attr.OAuthFlowStage(string(stage)))
	m.oauthFlowDeclinedCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

// RecordOAuthRefreshTokenReplayServed records a successful response from the
// refresh replay cache, dimensioned by issuer and MCP endpoint.
func (m *Metrics) RecordOAuthRefreshTokenReplayServed(ctx context.Context, issuerID, mcpSlug string) {
	if m == nil || m.oauthRefreshTokenReplayServedCounter == nil {
		return
	}
	m.oauthRefreshTokenReplayServedCounter.Add(ctx, 1, metric.WithAttributes(oauthFlowDimensions(issuerID, mcpSlug)...))
}

// MetaDispatchOutcome classifies how a meta member dispatch was credentialed.
type MetaDispatchOutcome string

const (
	// MetaDispatchCredentialed: a member-qualified credential was forwarded.
	MetaDispatchCredentialed MetaDispatchOutcome = "credentialed"
	// MetaDispatchAnonymous: no credential matched; the call went out bare.
	MetaDispatchAnonymous MetaDispatchOutcome = "anonymous"
	// MetaDispatchAmbiguous: several credentials claimed the member; the
	// dispatch failed closed.
	MetaDispatchAmbiguous MetaDispatchOutcome = "ambiguous"
)

// RecordMetaMemberDispatch counts one meta member dispatch.
func (m *Metrics) RecordMetaMemberDispatch(ctx context.Context, backend string, outcome MetaDispatchOutcome) {
	if m == nil || m.metaMemberDispatchCounter == nil {
		return
	}
	m.metaMemberDispatchCounter.Add(ctx, 1, metric.WithAttributes(
		attr.MetaMemberBackend(backend),
		attr.MetaDispatchOutcome(outcome),
	))
}
