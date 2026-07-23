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
	meterTypedEvents           = "risk.prompt_injection.typed_events"
	meterTypedDetections       = "risk.prompt_injection.typed_detections"
	meterTypedVerdicts         = "risk.prompt_injection.typed_verdicts"
	meterTypedContextCoverage  = "risk.prompt_injection.typed_context_coverage"
	meterTypedContextFields    = "risk.prompt_injection.typed_context_fields"
	meterTypedPhysicalCalls    = "risk.prompt_injection.typed_physical_calls"
	meterTypedCallDuration     = "risk.prompt_injection.typed_call_duration"
	meterTypedDecisionDuration = "risk.prompt_injection.typed_decision_duration"
	meterTypedFailOpen         = "risk.prompt_injection.typed_fail_open_samples"
	meterRateLimited           = "risk.prompt_injection.rate_limited"
)

type metrics struct {
	classifications  metric.Int64Counter
	duration         metric.Float64Histogram
	confidence       metric.Float64Histogram
	events           metric.Int64Counter
	detections       metric.Int64Counter
	verdicts         metric.Int64Counter
	contextCoverage  metric.Int64Counter
	contextFields    metric.Int64Counter
	physicalCalls    metric.Int64Counter
	callDuration     metric.Float64Histogram
	decisionDuration metric.Float64Histogram
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

	events, err := meter.Int64Counter(
		meterTypedEvents,
		metric.WithDescription("Typed prompt-injection judge events by finding, fail-open, and session-context state"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedEvents), attr.SlogError(err))
	}

	detections, err := meter.Int64Counter(
		meterTypedDetections,
		metric.WithDescription("Prompt-injection findings emitted to risk policy by typed evidence"),
		metric.WithUnit("{detection}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedDetections), attr.SlogError(err))
	}

	verdicts, err := meter.Int64Counter(
		meterTypedVerdicts,
		metric.WithDescription("All typed prompt-injection verdicts, including verdicts suppressed by the detection predicate"),
		metric.WithUnit("{verdict}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedVerdicts), attr.SlogError(err))
	}

	contextCoverage, err := meter.Int64Counter(
		meterTypedContextCoverage,
		metric.WithDescription("Typed prompt-injection trajectory context coverage by bounded presence state"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedContextCoverage), attr.SlogError(err))
	}

	contextFields, err := meter.Int64Counter(
		meterTypedContextFields,
		metric.WithDescription("Typed prompt-injection trajectory fields by presence and truncation state"),
		metric.WithUnit("{field}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedContextFields), attr.SlogError(err))
	}

	physicalCalls, err := meter.Int64Counter(
		meterTypedPhysicalCalls,
		metric.WithDescription("Physical model calls made by the typed prompt-injection judge"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedPhysicalCalls), attr.SlogError(err))
	}

	callDuration, err := meter.Float64Histogram(
		meterTypedCallDuration,
		metric.WithDescription("Physical typed prompt-injection model-call duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 7.5, 9, 10, 15),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedCallDuration), attr.SlogError(err))
	}

	decisionDuration, err := meter.Float64Histogram(
		meterTypedDecisionDuration,
		metric.WithDescription("End-to-end typed prompt-injection decision duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 7.5, 9, 10, 15),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedDecisionDuration), attr.SlogError(err))
	}

	failOpen, err := meter.Int64Counter(
		meterTypedFailOpen,
		metric.WithDescription("Typed prompt-injection judge samples that failed open, by bounded failure reason"),
		metric.WithUnit("{sample}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterTypedFailOpen), attr.SlogError(err))
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
		events:           events,
		detections:       detections,
		verdicts:         verdicts,
		contextCoverage:  contextCoverage,
		contextFields:    contextFields,
		physicalCalls:    physicalCalls,
		callDuration:     callDuration,
		decisionDuration: decisionDuration,
		failOpen:         failOpen,
		rateLimited:      rateLimited,
	}
}

func (m *metrics) RecordContext(ctx context.Context, orgID, model, reasoning string, priorPresent, recentPresent, priorTruncated, recentTruncated bool) {
	coverage := "neither"
	if priorPresent && recentPresent {
		coverage = "both"
	} else if priorPresent || recentPresent {
		coverage = "either"
	}
	common := []attribute.KeyValue{
		attr.OrganizationID(orgID),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
	}
	if m.contextCoverage != nil {
		m.contextCoverage.Add(ctx, 1, metric.WithAttributes(
			append(common,
				attribute.Bool("prior_user_request_present", priorPresent),
				attribute.Bool("recent_untrusted_content_present", recentPresent),
				attribute.String("coverage", coverage),
			)...,
		))
	}
	if m.contextFields != nil {
		m.contextFields.Add(ctx, 1, metric.WithAttributes(
			append(common,
				attribute.String("field", "prior_user_request"),
				attribute.Bool("present", priorPresent),
				attribute.Bool("truncated", priorTruncated),
			)...,
		))
		m.contextFields.Add(ctx, 1, metric.WithAttributes(
			append(common,
				attribute.String("field", "recent_untrusted_content"),
				attribute.Bool("present", recentPresent),
				attribute.Bool("truncated", recentTruncated),
			)...,
		))
	}
}

func (m *metrics) RecordPhysicalCall(ctx context.Context, orgID, model, reasoning string, outcome o11y.Outcome, failureReason string, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.Outcome(outcome),
		attribute.String("failure_reason", failureReason),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
	)
	if m.physicalCalls != nil {
		m.physicalCalls.Add(ctx, 1, attrs)
	}
	if m.callDuration != nil {
		m.callDuration.Record(ctx, duration.Seconds(), attrs)
	}
}

func (m *metrics) RecordEvent(ctx context.Context, orgID, model, reasoning string, sessionContextPresent, findingSurfaced, failOpen bool, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
		attribute.Bool("session_context_present", sessionContextPresent),
		attribute.Bool("finding_surfaced", findingSurfaced),
		attribute.Bool("fail_open", failOpen),
	)
	if m.events != nil {
		m.events.Add(ctx, 1, attrs)
	}
	if m.decisionDuration != nil {
		m.decisionDuration.Record(ctx, duration.Seconds(), attrs)
	}
}

func (m *metrics) RecordVerdict(ctx context.Context, orgID, kind, target string, operational, findingSurfaced, sessionContextPresent, failOpen bool, model, reasoning string) {
	if m.verdicts == nil {
		return
	}
	m.verdicts.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("directive_kind", kind),
		attribute.String("target", target),
		attribute.Bool("operational", operational),
		attribute.Bool("finding_surfaced", findingSurfaced),
		attribute.Bool("session_context_present", sessionContextPresent),
		attribute.Bool("fail_open", failOpen),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
	))
}

func (m *metrics) RecordFailOpen(ctx context.Context, orgID, model, reasoning, reason string) {
	if m.failOpen == nil {
		return
	}
	m.failOpen.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
		attribute.String("reason", reason),
	))
}

func (m *metrics) RecordDetection(ctx context.Context, orgID, kind, target string, operational bool, model, reasoning string) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("directive_kind", kind),
		attribute.String("target", target),
		attribute.Bool("operational", operational),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
	)
	if m.detections != nil {
		m.detections.Add(ctx, 1, attrs)
	}
}

// RecordClassification records one completed judge call: a count tagged by
// verdict label + cascade stage + outcome, and the call latency.
func (m *metrics) RecordClassification(ctx context.Context, orgID, label, model, reasoning string, outcome o11y.Outcome, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("label", label),
		attribute.String("stage", stageJudge),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
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
func (m *metrics) RecordRateLimited(ctx context.Context, orgID, model, reasoning string) {
	if m.rateLimited == nil {
		return
	}
	m.rateLimited.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("model", model),
		attribute.String("reasoning", reasoning),
	))
}
