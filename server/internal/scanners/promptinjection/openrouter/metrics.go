package openrouter

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterClassifications = "risk.prompt_injection.classifications"
	meterJudgeDuration   = "risk.prompt_injection.judge_duration"
	meterJudgeConfidence = "risk.prompt_injection.judge_confidence"
	meterRateLimited     = "risk.prompt_injection.rate_limited"
)

type metrics struct {
	classifications metric.Int64Counter
	duration        metric.Float64Histogram
	confidence      metric.Float64Histogram
	rateLimited     metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter")

	classifications, err := meter.Int64Counter(
		meterClassifications,
		metric.WithDescription("Prompt-injection judge classifications, tagged by verdict label and cascade stage"),
		metric.WithUnit("{classification}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterClassifications), attr.SlogError(err))
	}

	duration, err := meter.Float64Histogram(
		meterJudgeDuration,
		metric.WithDescription("Duration of a prompt-injection judge completion call in seconds"),
		metric.WithUnit("s"),
		// Buckets skew toward the 10s call timeout (judgeTimeout): sub-0.5s
		// resolution is useless when a stuck call is capped at 10s, so those
		// boundaries are traded for finer resolution as latency approaches the
		// timeout. The explicit 10s boundary means timed-out calls pile into the
		// (9, 10] bucket with outcome=timeout.
		metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 7.5, 9, 10, 15, 30, 60),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterJudgeDuration), attr.SlogError(err))
	}

	confidence, err := meter.Float64Histogram(
		meterJudgeConfidence,
		metric.WithDescription("Confidence score distribution for prompt-injection verdicts"),
		metric.WithUnit("{ratio}"),
		metric.WithExplicitBucketBoundaries(0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterJudgeConfidence), attr.SlogError(err))
	}

	rateLimited, err := meter.Int64Counter(
		meterRateLimited,
		metric.WithDescription("Number of prompt-injection judge calls rejected by the per-org rate limiter"),
		metric.WithUnit("{classification}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRateLimited), attr.SlogError(err))
	}

	return &metrics{
		classifications: classifications,
		duration:        duration,
		confidence:      confidence,
		rateLimited:     rateLimited,
	}
}

// RecordClassification records one completed judge call: a count tagged by
// verdict label + cascade stage + outcome, and the call latency.
func (m *metrics) RecordClassification(ctx context.Context, orgID, label string, outcome o11y.Outcome, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("label", label),
		attribute.String("stage", stageJudge),
		attr.Outcome(outcome),
	)
	if m.classifications != nil {
		m.classifications.Add(ctx, 1, attrs)
	}
	if m.duration != nil {
		m.duration.Record(ctx, duration.Seconds(), attrs)
	}
}

// RecordConfidence records the confidence score of an injection verdict.
func (m *metrics) RecordConfidence(ctx context.Context, orgID string, confidence float64) {
	if m.confidence == nil {
		return
	}
	m.confidence.Record(ctx, confidence, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("stage", stageJudge),
	))
}

// RecordRateLimited records a judge call rejected by the rate limiter.
func (m *metrics) RecordRateLimited(ctx context.Context, orgID string) {
	if m.rateLimited == nil {
		return
	}
	m.rateLimited.Add(ctx, 1, metric.WithAttributes(attr.OrganizationID(orgID)))
}
