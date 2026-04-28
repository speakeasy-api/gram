package risk_analysis

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterRiskScanEvents          = "risk.scan.events"
	meterRiskScanDuration        = "risk.scan.duration"
	meterRiskRuleConfidence      = "risk.rule.confidence"
	meterRiskPresidioScanSkipped = "risk.presidio.scan_skipped"
)

type riskMetrics struct {
	scanEvents          metric.Int64Counter
	scanDuration        metric.Float64Histogram
	ruleConfidence      metric.Float64Histogram
	presidioScanSkipped metric.Int64Counter
}

func newRiskMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *riskMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis")

	scanEvents, err := meter.Int64Counter(
		meterRiskScanEvents,
		metric.WithDescription("Total chat messages scanned by risk analysis"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskScanEvents), attr.SlogError(err))
	}

	scanDuration, err := meter.Float64Histogram(
		meterRiskScanDuration,
		metric.WithDescription("Duration of risk analysis scan per batch in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskScanDuration), attr.SlogError(err))
	}

	ruleConfidence, err := meter.Float64Histogram(
		meterRiskRuleConfidence,
		metric.WithDescription("Confidence score distribution for risk analysis findings"),
		metric.WithUnit("{ratio}"),
		metric.WithExplicitBucketBoundaries(0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskRuleConfidence), attr.SlogError(err))
	}

	presidioScanSkipped, err := meter.Int64Counter(
		meterRiskPresidioScanSkipped,
		metric.WithDescription("Number of batches where Presidio scanning was skipped due to errors"),
		metric.WithUnit("{batch}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskPresidioScanSkipped), attr.SlogError(err))
	}

	return &riskMetrics{
		scanEvents:          scanEvents,
		scanDuration:        scanDuration,
		ruleConfidence:      ruleConfidence,
		presidioScanSkipped: presidioScanSkipped,
	}
}

// RecordScan records metrics for a completed batch scan.
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

// RecordFindingConfidence records the confidence score of an individual finding.
func (m *riskMetrics) RecordFindingConfidence(ctx context.Context, orgID string, ruleID string, confidence float64) {
	if m.ruleConfidence == nil {
		return
	}
	m.ruleConfidence.Record(ctx, confidence, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.RiskRuleID(ruleID),
	))
}
