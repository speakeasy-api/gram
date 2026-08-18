package cimd

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterFetchAttempts        = "cimd.fetch.attempts"
	meterFetchDurationSeconds = "cimd.fetch.duration_seconds"
	meterFetchResponseSize    = "cimd.fetch.response_size"
	meterValidationFailures   = "cimd.validation.failures"
)

// fetchResult labels the outcome of one Resolve attempt on the cimd.fetch.*
// metrics. Every Resolve call records exactly one cimd.fetch.attempts point;
// cimd.fetch.duration_seconds is recorded only for results where an upstream
// fetch actually ran, so short-circuit results never skew latency percentiles.
//
// An admission_denied result is deliberately absent from this vocabulary.
// Admission control records to its own cimd.admission.decisions counter
// (internal/usersessions/cimd/admission): a denial means no fetch ran at all,
// so counting it under fetch.attempts would break this instrument's
// one-point-per-Resolve invariant and quietly change the denominator of every
// fetch-success chart.
type fetchResult string

const (
	fetchResultSuccess         fetchResult = "success"
	fetchResultFetchError      fetchResult = "fetch_error"
	fetchResultParseError      fetchResult = "parse_error"
	fetchResultValidationError fetchResult = "validation_error"

	// The cache results describe resolutions that did not fetch a fresh
	// document: cached served the caller's stored row without any upstream
	// request, conditional_not_modified made a conditional request the
	// upstream answered 304. Most points on this instrument are cached
	// once a client is warm, so a fetch-failure ratio must be taken against
	// the fetch-bearing outcomes rather than against every attempt.
	fetchResultCached                 fetchResult = "cached"
	fetchResultConditionalNotModified fetchResult = "conditional_not_modified"
)

// validationReason is the machine-readable label recorded on
// cimd.validation.failures for each rejection site in validate.go.
type validationReason string

const (
	reasonClientIDTooLong        validationReason = "client_id_too_long"
	reasonClientIDScheme         validationReason = "client_id_scheme"
	reasonClientIDFragment       validationReason = "client_id_fragment"
	reasonClientIDUnparseable    validationReason = "client_id_unparseable"
	reasonClientIDUserinfo       validationReason = "client_id_userinfo"
	reasonClientIDMissingHost    validationReason = "client_id_missing_host"
	reasonClientIDMissingPath    validationReason = "client_id_missing_path"
	reasonClientIDDotSegments    validationReason = "client_id_dot_segments"
	reasonClientIDMismatch       validationReason = "client_id_mismatch"
	reasonMissingClientName      validationReason = "missing_client_name"
	reasonClientNameTooLong      validationReason = "client_name_too_long"
	reasonInvalidAuthMethod      validationReason = "invalid_auth_method"
	reasonContainsSecret         validationReason = "contains_secret"
	reasonJWKSInvalid            validationReason = "jwks_invalid"
	reasonJWKSPrivateKey         validationReason = "jwks_private_key"
	reasonJWKSSymmetricKey       validationReason = "jwks_symmetric_key"
	reasonMissingRedirectURIs    validationReason = "missing_redirect_uris"
	reasonTooManyRedirectURIs    validationReason = "too_many_redirect_uris"
	reasonRedirectURITooLong     validationReason = "redirect_uri_too_long"
	reasonRedirectURIInvalid     validationReason = "redirect_uri_invalid"
	reasonRedirectOriginMismatch validationReason = "redirect_origin_mismatch"
)

type metrics struct {
	fetchAttempts      metric.Int64Counter
	fetchDuration      metric.Float64Histogram
	fetchResponseSize  metric.Int64Histogram
	validationFailures metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/usersessions/cimd")

	fetchAttempts, err := meter.Int64Counter(
		meterFetchAttempts,
		metric.WithDescription("Count of CIMD metadata document resolve attempts by origin and result"),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFetchAttempts), attr.SlogError(err))
	}

	fetchDuration, err := meter.Float64Histogram(
		meterFetchDurationSeconds,
		metric.WithDescription("Duration of CIMD metadata document resolution in seconds, recorded only when an upstream fetch ran"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFetchDurationSeconds), attr.SlogError(err))
	}

	fetchResponseSize, err := meter.Int64Histogram(
		meterFetchResponseSize,
		metric.WithDescription("Bytes of CIMD metadata document body read before the size cap, or the cap itself when hit"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(256, 512, 1024, 2048, 4096, maxDocumentBytes),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFetchResponseSize), attr.SlogError(err))
	}

	validationFailures, err := meter.Int64Counter(
		meterValidationFailures,
		metric.WithDescription("Count of CIMD metadata document validation failures by reason"),
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

// RecordAttempt counts one Resolve attempt. origin may be empty (pre-parse
// URL-syntax rejections have no established origin); the attribute is omitted
// then rather than recorded as "".
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
	m.fetchResponseSize.Record(ctx, bytes, metric.WithAttributes(attr.CIMDOrigin(origin)))
}

func (m *metrics) RecordValidationFailure(ctx context.Context, reason validationReason) {
	if m == nil || m.validationFailures == nil || reason == "" {
		return
	}
	m.validationFailures.Add(ctx, 1, metric.WithAttributes(attr.CIMDValidationReason(reason)))
}

func fetchAttributes(origin string, result fetchResult) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attr.Outcome(result)}
	if origin != "" {
		attrs = append(attrs, attr.CIMDOrigin(origin))
	}
	return attrs
}
