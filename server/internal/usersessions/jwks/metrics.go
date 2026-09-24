package jwks

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterFetchAttempts        = "jwks.fetch.attempts"
	meterFetchDurationSeconds = "jwks.fetch.duration_seconds"
	meterFetchResponseSize    = "jwks.fetch.response_size"
	meterValidationFailures   = "jwks.validation.failures"
)

// fetchResult labels the outcome of one Resolve attempt on the jwks.fetch.*
// metrics, mirroring the cimd vocabulary. Every Resolve call records exactly
// one jwks.fetch.attempts point; jwks.fetch.duration_seconds is recorded only
// for results where an upstream fetch actually ran, so short-circuit results
// never skew latency percentiles.
type fetchResult string

const (
	fetchResultSuccess         fetchResult = "success"
	fetchResultFetchError      fetchResult = "fetch_error"
	fetchResultFetchDenied     fetchResult = "fetch_denied"
	fetchResultParseError      fetchResult = "parse_error"
	fetchResultValidationError fetchResult = "validation_error"

	// The no-fetch results: inline resolved an inline document, cached
	// served the caller's stored state without any upstream request, and
	// conditional_not_modified made a conditional request the upstream
	// answered 304. Most points on a warm source are cached, so a
	// fetch-failure ratio must be taken against the fetch-bearing outcomes
	// rather than against every attempt.
	fetchResultInline                 fetchResult = "inline"
	fetchResultCached                 fetchResult = "cached"
	fetchResultConditionalNotModified fetchResult = "conditional_not_modified"
)

// validationReason is the machine-readable label recorded on
// jwks.validation.failures for each screening rejection. Parse failures
// (ErrKeySetInvalid) deliberately carry no reason: they are counted as
// parse_error on the fetch metrics, mirroring cimd, and the validation
// counter records only well-formed sets the screening rules refused.
type validationReason string

const (
	reasonPrivateKey   validationReason = "private_key"
	reasonSymmetricKey validationReason = "symmetric_key"
)

// resultOfParseError classifies a parseKeySet failure: rejected key material
// is a validation error (the host served a well-formed set that the screening
// rules refuse), anything else is a parse error (the host served something
// that is not a JWK Set).
func resultOfParseError(err error) fetchResult {
	if errors.Is(err, ErrPrivateKeyMaterial) || errors.Is(err, ErrSymmetricKeyMaterial) {
		return fetchResultValidationError
	}
	return fetchResultParseError
}

// validationReasonOf maps a parseKeySet failure onto the reason vocabulary.
func validationReasonOf(err error) validationReason {
	switch {
	case errors.Is(err, ErrPrivateKeyMaterial):
		return reasonPrivateKey
	case errors.Is(err, ErrSymmetricKeyMaterial):
		return reasonSymmetricKey
	default:
		return ""
	}
}

type metrics struct {
	fetchAttempts      metric.Int64Counter
	fetchDuration      metric.Float64Histogram
	fetchResponseSize  metric.Int64Histogram
	validationFailures metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/usersessions/jwks")

	fetchAttempts, err := meter.Int64Counter(
		meterFetchAttempts,
		metric.WithDescription("Count of remote JWK Set resolve attempts by origin and result"),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFetchAttempts), attr.SlogError(err))
	}

	fetchDuration, err := meter.Float64Histogram(
		meterFetchDurationSeconds,
		metric.WithDescription("Duration of remote JWK Set resolution in seconds, recorded only when an upstream fetch ran"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFetchDurationSeconds), attr.SlogError(err))
	}

	fetchResponseSize, err := meter.Int64Histogram(
		meterFetchResponseSize,
		metric.WithDescription("Bytes of JWK Set body read before the size cap, or the cap itself when hit"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(1024, 4096, 16384, 65536, 131072, maxKeySetBytes),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFetchResponseSize), attr.SlogError(err))
	}

	validationFailures, err := meter.Int64Counter(
		meterValidationFailures,
		metric.WithDescription("Count of JWK Set screening failures by reason"),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterValidationFailures), attr.SlogError(err))
	}

	return &metrics{
		fetchAttempts:      fetchAttempts,
		fetchDuration:      fetchDuration,
		fetchResponseSize:  fetchResponseSize,
		validationFailures: validationFailures,
	}
}

// RecordAttempt counts one Resolve attempt. origin may be empty (inline
// sources have none); the attribute is omitted then rather than recorded as
// "".
func (m *metrics) RecordAttempt(ctx context.Context, origin string, result fetchResult) {
	if m == nil || m.fetchAttempts == nil {
		return
	}
	m.fetchAttempts.Add(ctx, 1, metric.WithAttributes(fetchAttributes(origin, result)...))
}

func (m *metrics) RecordFetchDuration(ctx context.Context, origin string, result fetchResult, duration time.Duration) {
	if m == nil || m.fetchDuration == nil {
		return
	}
	m.fetchDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(fetchAttributes(origin, result)...))
}

func (m *metrics) RecordResponseSize(ctx context.Context, origin string, bytes int64) {
	if m == nil || m.fetchResponseSize == nil {
		return
	}
	m.fetchResponseSize.Record(ctx, bytes, metric.WithAttributes(attr.JWKSOrigin(origin)))
}

func (m *metrics) RecordValidationFailure(ctx context.Context, reason validationReason) {
	if m == nil || m.validationFailures == nil || reason == "" {
		return
	}
	m.validationFailures.Add(ctx, 1, metric.WithAttributes(attr.JWKSValidationReason(reason)))
}

func fetchAttributes(origin string, result fetchResult) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attr.Outcome(result)}
	if origin != "" {
		attrs = append(attrs, attr.JWKSOrigin(origin))
	}
	return attrs
}
