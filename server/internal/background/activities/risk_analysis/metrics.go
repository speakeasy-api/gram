package risk_analysis

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

const (
	meterRiskScanEvents                  = "risk.scan.events"
	meterRiskScanDuration                = "risk.scan.duration"
	meterRiskRecommendedScopePrefiltered = "risk.recommended_scope.messages_prefiltered"
	meterRiskLLMPolicyEvaluations        = "risk.llm.policy_evaluations"
	meterRiskBatchInputTruncations       = "risk.batch.input_truncations"
)

type riskMetrics struct {
	scanEvents                          metric.Int64Counter
	scanDuration                        metric.Float64Histogram
	recommendedScopeMessagesPrefiltered metric.Int64Counter
	llmPolicyEvaluations                metric.Int64Counter
	batchInputTruncations               metric.Int64Counter
}

func newRiskMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *riskMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis")

	scanEvents, err := meter.Int64Counter(
		meterRiskScanEvents,
		metric.WithDescription("Messages and content parts published for risk analysis"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskScanEvents), attr.SlogError(err))
	}

	scanDuration, err := meter.Float64Histogram(
		meterRiskScanDuration,
		metric.WithDescription("Duration of a risk analysis batch publish in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskScanDuration), attr.SlogError(err))
	}

	recommendedScopeMessagesPrefiltered, err := meter.Int64Counter(
		meterRiskRecommendedScopePrefiltered,
		metric.WithDescription("Messages skipped before expensive scanners by recommended category scopes"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskRecommendedScopePrefiltered), attr.SlogError(err))
	}

	llmPolicyEvaluations, err := meter.Int64Counter(
		meterRiskLLMPolicyEvaluations,
		metric.WithDescription("Policy evaluations routed to the fine-tuned LLM risk analyzer, by lane and outcome"),
		metric.WithUnit("{evaluation}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskLLMPolicyEvaluations), attr.SlogError(err))
	}

	batchInputTruncations, err := meter.Int64Counter(
		meterRiskBatchInputTruncations,
		metric.WithDescription("Batch scan inputs cut to the 50 KiB load-time bound"),
		metric.WithUnit("{input}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskBatchInputTruncations), attr.SlogError(err))
	}

	return &riskMetrics{
		scanEvents:                          scanEvents,
		scanDuration:                        scanDuration,
		recommendedScopeMessagesPrefiltered: recommendedScopeMessagesPrefiltered,
		llmPolicyEvaluations:                llmPolicyEvaluations,
		batchInputTruncations:               batchInputTruncations,
	}
}

// RecordScan records metrics for a completed batch publish.
func (m *riskMetrics) RecordScan(ctx context.Context, orgID string, outcome o11y.Outcome, messagesScanned int, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.Outcome(outcome),
	)

	if m.scanEvents != nil {
		m.scanEvents.Add(ctx, int64(messagesScanned), attrs)
	}

	if m.scanDuration != nil {
		m.scanDuration.Record(ctx, duration.Seconds(), attrs)
	}
}

func (m *riskMetrics) RecordRecommendedScopePrefiltered(ctx context.Context, orgID, source string, count int) {
	if count <= 0 || m == nil || m.recommendedScopeMessagesPrefiltered == nil {
		return
	}
	m.recommendedScopeMessagesPrefiltered.Add(ctx, int64(count), metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.RiskSource(source),
		attribute.Int("risk.recommended_scopes.version", recommendedscopes.Version),
	))
}

// RecordLLMPolicyEvaluation counts the policy evaluations the batch lane
// routed to the fine-tuned LLM risk analyzer, one per published message so
// the count lines up with the consumer's per-request outcomes. The batch side
// only knows whether the requests were published; verdict outcomes are
// recorded by the consumer.
func (m *riskMetrics) RecordLLMPolicyEvaluation(ctx context.Context, orgID string, policyID string, outcome string, count int) {
	if m == nil || m.llmPolicyEvaluations == nil || count <= 0 {
		return
	}
	m.llmPolicyEvaluations.Add(ctx, int64(count), metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.RiskPolicyID(policyID),
		attr.RiskScanMode(llmanalyzer.ScanModeAsync),
		attr.Outcome(outcome),
	))
}

func (m *riskMetrics) RecordBatchInputTruncation(ctx context.Context, orgID string, contentPart bool) {
	if m == nil || m.batchInputTruncations == nil {
		return
	}
	source := "message"
	if contentPart {
		source = "content_part"
	}
	m.batchInputTruncations.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("risk.batch.input_source", source),
	))
}
