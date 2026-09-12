package registration

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const failureMetric = "gram.oauth.client_registration.failures"
const failureEvent = "oauth.client_registration.failure"

// Recorder receives bounded automatic client-registration failures.
type Recorder interface {
	RecordFailure(ctx context.Context, method Method, failure Failure)
}

// Metrics records automatic client-registration failures without tenant,
// provider, client, message, credential, or request data.
type Metrics struct {
	logger   *slog.Logger
	failures metric.Int64Counter
}

func NewMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *Metrics {
	metrics := &Metrics{logger: logger, failures: nil}
	if meterProvider == nil {
		return metrics
	}
	failures, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oauth/registration").Int64Counter(
		failureMetric,
		metric.WithDescription("Automatic OAuth client registration failures by bounded method and classification"),
		metric.WithUnit("{failure}"),
	)
	if err != nil && logger != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(failureMetric), attr.SlogError(err))
	}
	metrics.failures = failures
	return metrics
}

// RecordFailure increments the failure counter only for a valid bounded
// contract. HTTP status is the sole optional dimension.
func (m *Metrics) RecordFailure(ctx context.Context, method Method, failure Failure) {
	if m == nil || !validMethod(method) || !validFailure(failure) {
		return
	}
	attributes := []attribute.KeyValue{
		attr.OAuthRegistrationMethod(method),
		attr.OAuthRegistrationOutcome(failure.Outcome),
		attr.OAuthRegistrationReason(failure.Reason),
		attr.OAuthRegistrationRetryable(failure.Retryable),
	}
	if failure.HTTPStatus != nil {
		attributes = append(attributes, attr.HTTPResponseStatusCode(*failure.HTTPStatus))
	}
	if m.failures != nil {
		m.failures.Add(ctx, 1, metric.WithAttributes(attributes...))
	}

	if m.logger == nil {
		return
	}
	logAttributes := []slog.Attr{
		attr.SlogEvent(failureEvent),
		attr.SlogOAuthRegistrationMethod(method),
		attr.SlogOAuthRegistrationOutcome(failure.Outcome),
		attr.SlogOAuthRegistrationReason(failure.Reason),
		attr.SlogOAuthRegistrationRetryable(failure.Retryable),
	}
	if failure.HTTPStatus != nil {
		logAttributes = append(logAttributes, attr.SlogHTTPResponseStatusCode(*failure.HTTPStatus))
	}
	m.logger.LogAttrs(ctx, slog.LevelWarn, "oauth client registration failed", logAttributes...)
}

func validMethod(method Method) bool {
	return method == MethodDCR || method == MethodCIMD
}

func validFailure(failure Failure) bool {
	if failure.HTTPStatus != nil && (*failure.HTTPStatus < 100 || *failure.HTTPStatus > 599) {
		return false
	}
	switch failure.Outcome {
	case OutcomeUnreachable:
		if !failure.Retryable {
			return false
		}
		switch failure.Reason {
		case ReasonDNSError, ReasonTLSError, ReasonTimeout, ReasonNetworkError, ReasonRateLimited, ReasonUpstreamUnavailable:
			return true
		case ReasonAuthorizationRejected, ReasonInvalidSuccessResponse:
			return false
		}
	case OutcomeRefused:
		if failure.Retryable {
			return false
		}
		return failure.Reason == ReasonAuthorizationRejected || failure.Reason == ReasonInvalidSuccessResponse
	default:
		return false
	}
	return false
}
