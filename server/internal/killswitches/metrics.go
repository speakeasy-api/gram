package killswitches

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterKillswitchEvaluationDuration = "killswitch.evaluation.duration"

	killswitchEvaluationOutcomeMatched          = "matched"
	killswitchEvaluationOutcomeUnmatched        = "unmatched"
	killswitchEvaluationOutcomeEvaluatorFailure = "evaluator_failure"
)

type evaluationMetrics struct {
	duration               metric.Float64Histogram
	matchedOption          metric.RecordOption
	unmatchedOption        metric.RecordOption
	evaluatorFailureOption metric.RecordOption
}

func newEvaluationMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *evaluationMetrics {
	if meterProvider == nil {
		return nil
	}

	histogram, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/killswitches").Float64Histogram(
		meterKillswitchEvaluationDuration,
		metric.WithDescription("Duration of authoritative kill-switch evaluation in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterKillswitchEvaluationDuration), attr.SlogError(err))
		}
		return nil
	}
	return &evaluationMetrics{
		duration:               histogram,
		matchedOption:          metric.WithAttributes(attr.Outcome(killswitchEvaluationOutcomeMatched)),
		unmatchedOption:        metric.WithAttributes(attr.Outcome(killswitchEvaluationOutcomeUnmatched)),
		evaluatorFailureOption: metric.WithAttributes(attr.Outcome(killswitchEvaluationOutcomeEvaluatorFailure)),
	}
}

func (m *evaluationMetrics) record(ctx context.Context, outcome string, duration time.Duration) {
	var option metric.RecordOption
	switch outcome {
	case killswitchEvaluationOutcomeMatched:
		option = m.matchedOption
	case killswitchEvaluationOutcomeUnmatched:
		option = m.unmatchedOption
	case killswitchEvaluationOutcomeEvaluatorFailure:
		option = m.evaluatorFailureOption
	default:
		return
	}
	m.duration.Record(ctx, duration.Seconds(), option)
}
