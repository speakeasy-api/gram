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
	meterClassifications       = "risk.prompt_injection.classifications"
	meterJudgeDuration         = "risk.prompt_injection.judge_duration"
	meterJudgeConfidence       = "risk.prompt_injection.judge_confidence"
	meterConsensusSupport      = "risk.prompt_injection.consensus_support"
	meterRedesignDetections    = "risk.prompt_injection.redesign_detections"
	meterRedesignPhysicalCalls = "risk.prompt_injection.redesign_physical_calls"
	meterRedesignCallDuration  = "risk.prompt_injection.redesign_call_duration"
	meterRedesignFailOpen      = "risk.prompt_injection.redesign_fail_open_samples"
	meterRateLimited           = "risk.prompt_injection.rate_limited"
)

type metrics struct {
	classifications  metric.Int64Counter
	duration         metric.Float64Histogram
	confidence       metric.Float64Histogram
	consensus        metric.Float64Histogram
	detections       metric.Int64Counter
	physicalCalls    metric.Int64Counter
	redesignDuration metric.Float64Histogram
	failOpen         metric.Int64Counter
	rateLimited      metric.Int64Counter
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

	consensus, err := meter.Float64Histogram(
		meterConsensusSupport,
		metric.WithDescription("Consensus support ratio for surfaced PI redesign detections"),
		metric.WithUnit("{ratio}"),
		metric.WithExplicitBucketBoundaries(0, 0.34, 0.67, 1),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterConsensusSupport), attr.SlogError(err))
	}

	detections, err := meter.Int64Counter(
		meterRedesignDetections,
		metric.WithDescription("Surfaced PI redesign detections by typed evidence and shadow action"),
		metric.WithUnit("{detection}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRedesignDetections), attr.SlogError(err))
	}

	physicalCalls, err := meter.Int64Counter(
		meterRedesignPhysicalCalls,
		metric.WithDescription("Physical model calls made by the PI redesign sampler"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRedesignPhysicalCalls), attr.SlogError(err))
	}

	redesignDuration, err := meter.Float64Histogram(
		meterRedesignCallDuration,
		metric.WithDescription("Physical PI redesign model-call duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 7.5, 9, 10, 15),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRedesignCallDuration), attr.SlogError(err))
	}

	failOpen, err := meter.Int64Counter(
		meterRedesignFailOpen,
		metric.WithDescription("PI redesign samples converted to safe votes, by bounded failure reason"),
		metric.WithUnit("{sample}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRedesignFailOpen), attr.SlogError(err))
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
		classifications:  classifications,
		duration:         duration,
		confidence:       confidence,
		consensus:        consensus,
		detections:       detections,
		physicalCalls:    physicalCalls,
		redesignDuration: redesignDuration,
		failOpen:         failOpen,
		rateLimited:      rateLimited,
	}
}

func (m *metrics) RecordPhysicalCall(ctx context.Context, orgID string, outcome o11y.Outcome, failureReason string, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.Outcome(outcome),
		attribute.String("failure_reason", failureReason),
	)
	if m.physicalCalls != nil {
		m.physicalCalls.Add(ctx, 1, attrs)
	}
	if m.redesignDuration != nil {
		m.redesignDuration.Record(ctx, duration.Seconds(), attrs)
	}
}

func (m *metrics) RecordFailOpen(ctx context.Context, orgID, reason string) {
	if m.failOpen == nil {
		return
	}
	m.failOpen.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("reason", reason),
	))
}

func (m *metrics) RecordDetection(ctx context.Context, orgID, kind, target string, severity Severity, action Action) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("directive_kind", kind),
		attribute.String("target", target),
		attribute.String("severity", string(severity)),
		attribute.String("action", string(action)),
	)
	if m.detections != nil {
		m.detections.Add(ctx, 1, attrs)
	}
}

func (m *metrics) RecordConsensus(ctx context.Context, orgID string, support float64) {
	if m.consensus == nil {
		return
	}
	m.consensus.Record(ctx, support, metric.WithAttributes(attr.OrganizationID(orgID)))
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
