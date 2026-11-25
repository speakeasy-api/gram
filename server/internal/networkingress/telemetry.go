package networkingress

import (
	"context"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	OperationsMetric = "gram.network_ingress.operation"
	DurationMetric   = "gram.network_ingress.operation.duration"

	OperationAdmission      = "admission"
	OperationAttestation    = "attestation"
	OperationProxy          = "proxy"
	OperationResolution     = "resolution"
	OperationOAuthAuthority = "oauth_authority"
	OperationUnknown        = "unknown"

	ResultAllowed = "allowed"
	ResultDenied  = "denied"
	ResultError   = "error"
	ResultUnknown = "unknown"

	ReasonNone                 = "none"
	ReasonMissingAttestation   = "missing_attestation"
	ReasonInvalidSource        = "invalid_source"
	ReasonAttestationRejected  = "attestation_rejected"
	ReasonVerifierUnavailable  = "verifier_unavailable"
	ReasonHostMismatch         = "host_mismatch"
	ReasonIdentityInvalid      = "identity_invalid"
	ReasonIdentityRequired     = "identity_required"
	ReasonProviderUnsupported  = "provider_unsupported"
	ReasonOriginInvalid        = "origin_invalid"
	ReasonTokenReadFailed      = "token_read_failed"
	ReasonUpstreamFailed       = "upstream_failed"
	ReasonRateLimited          = "rate_limited"
	ReasonCacheHit             = "cache_hit"
	ReasonNegativeCacheHit     = "negative_cache_hit"
	ReasonTokenReviewDenied    = "token_review_denied"
	ReasonAuthorityRejected    = "authority_rejected"
	ReasonAuthorityUnavailable = "authority_unavailable"
	ReasonNamespaceRejected    = "namespace_rejected"
	ReasonEndpointNotFound     = "endpoint_not_found"
	ReasonPolicyDenied         = "policy_denied"
	ReasonDependencyFailed     = "dependency_failed"
	ReasonUnknown              = "unknown"
)

type Telemetry struct {
	logger     *slog.Logger
	operations metric.Int64Counter
	duration   metric.Float64Histogram
}

func NewTelemetry(logger *slog.Logger, meterProvider metric.MeterProvider) *Telemetry {
	if logger != nil {
		logger = logger.With(attr.SlogComponent("network_ingress_telemetry"))
	}
	if meterProvider == nil {
		return &Telemetry{
			logger:     logger,
			operations: nil,
			duration:   nil,
		}
	}
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/networkingress")
	operations, err := meter.Int64Counter(
		OperationsMetric,
		metric.WithDescription("Private network ingress trust-boundary operations by bounded operation, result, reason, provider, and network surface."),
		metric.WithUnit("{operation}"),
	)
	if err != nil && logger != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(OperationsMetric), attr.SlogError(err))
	}
	duration, err := meter.Float64Histogram(
		DurationMetric,
		metric.WithDescription("Private network ingress trust-boundary operation duration in seconds."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil && logger != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(DurationMetric), attr.SlogError(err))
	}
	return &Telemetry{logger: logger, operations: operations, duration: duration}
}

func (t *Telemetry) Record(ctx context.Context, operation, result, reason, provider string, duration time.Duration) {
	if t == nil {
		return
	}
	operation, result, reason, provider = clampOperation(operation), clampResult(result), clampReason(reason), clampProvider(provider)
	attributes := []attribute.KeyValue{
		attr.NetworkIngressOperation(operation),
		attr.NetworkIngressResult(result),
		attr.NetworkIngressReason(reason),
		attr.Provider(provider),
		attr.NetworkSurface("private"),
	}
	if t.operations != nil {
		t.operations.Add(ctx, 1, metric.WithAttributes(attributes...))
	}
	if t.duration != nil {
		t.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attributes...))
	}
	trace.SpanFromContext(ctx).SetAttributes(attributes...)
	if t.logger != nil && result != ResultAllowed {
		logAttrs := []any{
			attr.SlogNetworkIngressOperation(operation),
			attr.SlogNetworkIngressResult(result),
			attr.SlogNetworkIngressReason(reason),
			attr.SlogProvider(provider),
			attr.SlogNetworkSurface("private"),
		}
		if result == ResultError {
			t.logger.ErrorContext(ctx, "private network ingress operation failed", logAttrs...)
		} else {
			t.logger.WarnContext(ctx, "private network ingress operation denied", logAttrs...)
		}
	}
}

func clampOperation(value string) string {
	switch value {
	case OperationAdmission, OperationAttestation, OperationProxy, OperationResolution, OperationOAuthAuthority, OperationUnknown:
		return value
	default:
		return OperationUnknown
	}
}

func clampResult(value string) string {
	switch value {
	case ResultAllowed, ResultDenied, ResultError, ResultUnknown:
		return value
	default:
		return ResultUnknown
	}
}

func clampReason(value string) string {
	switch value {
	case ReasonNone, ReasonMissingAttestation, ReasonInvalidSource, ReasonAttestationRejected,
		ReasonVerifierUnavailable, ReasonHostMismatch, ReasonIdentityInvalid, ReasonIdentityRequired,
		ReasonProviderUnsupported, ReasonOriginInvalid, ReasonTokenReadFailed, ReasonUpstreamFailed,
		ReasonRateLimited, ReasonCacheHit, ReasonNegativeCacheHit, ReasonTokenReviewDenied,
		ReasonAuthorityRejected, ReasonAuthorityUnavailable, ReasonNamespaceRejected,
		ReasonEndpointNotFound, ReasonPolicyDenied, ReasonDependencyFailed, ReasonUnknown:
		return value
	default:
		return ReasonUnknown
	}
}

func clampProvider(value string) string {
	if value == "tailscale" {
		return value
	}
	return "unknown"
}
