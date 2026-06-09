package mcp

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// oauthFlowStage is the closed set of coarse stages at which a user-facing
// OAuth flow can terminally resolve to a non-completion outcome (failed or
// declined). It names the handler leg where the flow ended. Kept as a bounded
// enum — and deliberately decoupled from the free-form reason strings the
// handlers log — so the `gram.oauth.flow_stage` metric dimension stays
// low-cardinality. Add a value here only when a new terminal point is
// instrumented.
type oauthFlowStage string

const (
	// oauthFlowStageAuthorize: the flow died in /authorize after the challenge
	// was minted (e.g. building the IDP authorization URL failed, which
	// surfaces a misconfigured IDP).
	oauthFlowStageAuthorize oauthFlowStage = "authorize"
	// oauthFlowStageIDPCallback: the flow ended on the private-toolset IDP
	// return leg (HandleIDPCallback) — an IDP error, a failed code exchange,
	// an org-membership denial, or the user cancelling at the IDP.
	oauthFlowStageIDPCallback oauthFlowStage = "idp_callback"
	// oauthFlowStageConsent: the flow ended at the consent step (HandleConsent
	// POST) — the user declined, or the approval could not be persisted.
	oauthFlowStageConsent oauthFlowStage = "consent"
	// oauthFlowStageToken: the authorization_code token exchange was rejected
	// (HandleToken). Refresh-token grants are NOT part of a flow and never
	// record here.
	oauthFlowStageToken oauthFlowStage = "token"
)

type metrics struct {
	mcpToolCallCounter metric.Int64Counter
	mcpRequestDuration metric.Float64Histogram

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
}

func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
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

	return &metrics{
		mcpToolCallCounter:        mcpToolCallCounter,
		mcpRequestDuration:        mcpRequestDuration,
		oauthFlowStartedCounter:   oauthFlowStartedCounter,
		oauthFlowCompletedCounter: oauthFlowCompletedCounter,
		oauthFlowFailedCounter:    oauthFlowFailedCounter,
		oauthFlowDeclinedCounter:  oauthFlowDeclinedCounter,
	}
}

func (m *metrics) RecordMCPToolCall(ctx context.Context, orgID string, mcpURL string, toolName string) {
	if m.mcpToolCallCounter == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.McpURL(mcpURL),
		attr.ToolName(toolName),
		attr.OrganizationID(orgID),
	}
	m.mcpToolCallCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

func (m *metrics) RecordMCPRequestDuration(ctx context.Context, mcpMethod string, mcpURL string, duration time.Duration) {
	if m.mcpRequestDuration == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.McpMethod(mcpMethod),
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
func (m *metrics) RecordOAuthFlowStarted(ctx context.Context, issuerID, mcpSlug string) {
	if m.oauthFlowStartedCounter == nil {
		return
	}
	m.oauthFlowStartedCounter.Add(ctx, 1, metric.WithAttributes(oauthFlowDimensions(issuerID, mcpSlug)...))
}

// RecordOAuthFlowCompleted records that a user-facing OAuth flow resolved
// successfully — emitted when the authorization_code token exchange succeeds.
func (m *metrics) RecordOAuthFlowCompleted(ctx context.Context, issuerID, mcpSlug string) {
	if m.oauthFlowCompletedCounter == nil {
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
func (m *metrics) RecordOAuthFlowFailed(ctx context.Context, issuerID, mcpSlug string, stage oauthFlowStage) {
	if m.oauthFlowFailedCounter == nil {
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
func (m *metrics) RecordOAuthFlowDeclined(ctx context.Context, issuerID, mcpSlug string, stage oauthFlowStage) {
	if m.oauthFlowDeclinedCounter == nil {
		return
	}
	kv := append(oauthFlowDimensions(issuerID, mcpSlug), attr.OAuthFlowStage(string(stage)))
	m.oauthFlowDeclinedCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}
