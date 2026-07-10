package riskjudge

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterJudgeEvaluations = "risk.judge.evaluations"
	meterJudgeDuration    = "risk.judge.duration"
	meterJudgeConfidence  = "risk.judge.confidence"
)

type judgeMetrics struct {
	evaluations metric.Int64Counter
	duration    metric.Float64Histogram
	confidence  metric.Float64Histogram
}

func newJudgeMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *judgeMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/riskjudge")

	evaluations, err := meter.Int64Counter(
		meterJudgeEvaluations,
		metric.WithDescription("Total LLM judge evaluations that issued a completion call"),
		metric.WithUnit("{evaluation}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterJudgeEvaluations), attr.SlogError(err))
	}

	duration, err := meter.Float64Histogram(
		meterJudgeDuration,
		metric.WithDescription("Duration of an LLM judge completion call in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterJudgeDuration), attr.SlogError(err))
	}

	confidence, err := meter.Float64Histogram(
		meterJudgeConfidence,
		metric.WithDescription("Confidence score distribution for matched LLM judge verdicts"),
		metric.WithUnit("{ratio}"),
		metric.WithExplicitBucketBoundaries(0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterJudgeConfidence), attr.SlogError(err))
	}

	return &judgeMetrics{
		evaluations: evaluations,
		duration:    duration,
		confidence:  confidence,
	}
}

// RecordEvaluation records the outcome and latency of a completed judge call.
func (m *judgeMetrics) RecordEvaluation(ctx context.Context, orgID string, outcome o11y.Outcome, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.Outcome(outcome),
	)
	if m.evaluations != nil {
		m.evaluations.Add(ctx, 1, attrs)
	}
	if m.duration != nil {
		m.duration.Record(ctx, duration.Seconds(), attrs)
	}
}

// RecordConfidence records the confidence score of a matched verdict.
func (m *judgeMetrics) RecordConfidence(ctx context.Context, orgID string, confidence float64) {
	if m.confidence == nil {
		return
	}
	m.confidence.Record(ctx, confidence, metric.WithAttributes(attr.OrganizationID(orgID)))
}
