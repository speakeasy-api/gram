package platformmcp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

const (
	platformMCPOAuthEventMetric                   = "platform_mcp.oauth.events"
	platformMCPOAuthRefreshDurationMetric         = "platform_mcp.oauth.refresh_duration"
	platformMCPOAuthConnectionAgeMetric           = "platform_mcp.oauth.connection_age"
	platformMCPOAuthReauthorizationRequiredMetric = "platform_mcp.oauth.reauthorization_required"
)

type OAuthEvent struct {
	Operation string
	Outcome   string
	Reason    string
}

type OAuthTelemetry interface {
	Record(context.Context, OAuthEvent)
	RecordRefreshSuccess(context.Context, time.Duration, time.Duration)
	RecordTerminalTransition(context.Context, platformoauth.ReauthorizationReason)
}

type noopOAuthTelemetry struct{}

func (noopOAuthTelemetry) Record(context.Context, OAuthEvent)                                 {}
func (noopOAuthTelemetry) RecordRefreshSuccess(context.Context, time.Duration, time.Duration) {}
func (noopOAuthTelemetry) RecordTerminalTransition(context.Context, platformoauth.ReauthorizationReason) {
}

type oauthTelemetry struct {
	logger                  *slog.Logger
	events                  metric.Int64Counter
	refreshDuration         metric.Float64Histogram
	connectionAge           metric.Float64Histogram
	reauthorizationRequired metric.Int64Counter
}

func NewOAuthTelemetry(logger *slog.Logger, meterProvider metric.MeterProvider) OAuthTelemetry {
	if logger == nil || meterProvider == nil {
		return noopOAuthTelemetry{}
	}

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/platformmcp")
	events, err := meter.Int64Counter(platformMCPOAuthEventMetric, metric.WithDescription("Bounded Platform MCP OAuth outcomes"), metric.WithUnit("{event}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP OAuth metric", attr.SlogMetricName(platformMCPOAuthEventMetric), attr.SlogError(err))
	}
	refreshDuration, err := meter.Float64Histogram(platformMCPOAuthRefreshDurationMetric, metric.WithDescription("Successful Platform MCP refresh duration"), metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP OAuth metric", attr.SlogMetricName(platformMCPOAuthRefreshDurationMetric), attr.SlogError(err))
	}
	connectionAge, err := meter.Float64Histogram(platformMCPOAuthConnectionAgeMetric, metric.WithDescription("Age of a Platform MCP authorization generation at successful refresh"), metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(60, 300, 900, 3600, 21600, 86400, 604800, 2592000, 7776000))
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP OAuth metric", attr.SlogMetricName(platformMCPOAuthConnectionAgeMetric), attr.SlogError(err))
	}
	reauthorizationRequired, err := meter.Int64Counter(platformMCPOAuthReauthorizationRequiredMetric, metric.WithDescription("Committed Platform MCP reauthorization-required transitions"), metric.WithUnit("{transition}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP OAuth metric", attr.SlogMetricName(platformMCPOAuthReauthorizationRequiredMetric), attr.SlogError(err))
	}

	return &oauthTelemetry{logger: logger, events: events, refreshDuration: refreshDuration, connectionAge: connectionAge, reauthorizationRequired: reauthorizationRequired}
}

func (t *oauthTelemetry) Record(ctx context.Context, event OAuthEvent) {
	if t == nil || t.events == nil || !validOAuthEvent(event) {
		return
	}
	attributes := []attribute.KeyValue{
		attribute.String("platform_mcp.operation", event.Operation),
		attribute.String("platform_mcp.outcome", event.Outcome),
	}
	if event.Reason != "" {
		attributes = append(attributes, attribute.String("platform_mcp.reason", event.Reason))
	}
	t.events.Add(ctx, 1, metric.WithAttributes(attributes...))
}

func (t *oauthTelemetry) RecordRefreshSuccess(ctx context.Context, duration, connectionAge time.Duration) {
	if t == nil {
		return
	}
	attributes := metric.WithAttributes(attribute.String("platform_mcp.operation", "refresh"), attribute.String("platform_mcp.outcome", "succeeded"))
	if t.refreshDuration != nil {
		t.refreshDuration.Record(ctx, duration.Seconds(), attributes)
	}
	if t.connectionAge != nil && connectionAge >= 0 {
		t.connectionAge.Record(ctx, connectionAge.Seconds(), attributes)
	}
}

func (t *oauthTelemetry) RecordTerminalTransition(ctx context.Context, reason platformoauth.ReauthorizationReason) {
	if t == nil || t.reauthorizationRequired == nil || !validReauthorizationReason(reason) {
		return
	}
	t.reauthorizationRequired.Add(ctx, 1, metric.WithAttributes(attribute.String("platform_mcp.reason", string(reason))))
	t.logger.WarnContext(ctx, "platform mcp authorization requires reauthorization", attr.SlogEvent("platform_mcp.oauth.terminal_transition"), attr.SlogReason(string(reason)))
}

func validOAuthEvent(event OAuthEvent) bool {
	if event.Operation != "refresh" && event.Operation != "code_exchange" && event.Operation != "interactive_authorization" && event.Operation != "revocation" && event.Operation != "runtime_auth" {
		return false
	}
	switch event.Outcome {
	case "succeeded", "invalid_grant", "access_denied", "temporarily_unavailable", "invalid_client", "invalid_request", "server_error", "unsupported_grant_type", "unauthorized":
	default:
		return false
	}
	return event.Reason == "" || validOAuthReason(event.Reason)
}

func validOAuthReason(reason string) bool {
	if validReauthorizationReason(platformoauth.ReauthorizationReason(reason)) {
		return true
	}
	switch reason {
	case "not_found", "already_used", "expired", "revoked", "client_mismatch", "generation_invalid", "redirect_uri_invalid", "pkce_invalid", "authorization_denied", "platform_disabled":
		return true
	default:
		return false
	}
}

func validReauthorizationReason(reason platformoauth.ReauthorizationReason) bool {
	switch reason {
	case platformoauth.ReauthorizationReasonRefreshIdleExpired, platformoauth.ReauthorizationReasonAuthorizationExpired, platformoauth.ReauthorizationReasonRefreshReuse, platformoauth.ReauthorizationReasonConnectionRevoked, platformoauth.ReauthorizationReasonClientRevoked, platformoauth.ReauthorizationReasonAuthorizationLost, platformoauth.ReauthorizationReasonSecurityReset:
		return true
	default:
		return false
	}
}

func oauthStateFailureReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, platformoauth.ErrNotFound):
		return "not_found"
	case errors.Is(err, platformoauth.ErrAlreadyUsed):
		return "already_used"
	case errors.Is(err, platformoauth.ErrExpired):
		return "expired"
	case errors.Is(err, platformoauth.ErrRevoked):
		return "revoked"
	case errors.Is(err, platformoauth.ErrClientMismatch):
		return "client_mismatch"
	case errors.Is(err, platformoauth.ErrGeneration):
		return "generation_invalid"
	case errors.Is(err, platformoauth.ErrRedirectURI):
		return "redirect_uri_invalid"
	case errors.Is(err, platformoauth.ErrPKCE):
		return "pkce_invalid"
	default:
		return ""
	}
}
