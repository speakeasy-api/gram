package risk_analysis

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
)

const (
	meterRiskScanEvents                         = "risk.scan.events"
	meterRiskScanDuration                       = "risk.scan.duration"
	meterRiskRuleConfidence                     = "risk.rule.confidence"
	meterRiskPresidioScanSkipped                = "risk.presidio.scan_skipped"
	meterRiskRecommendedScopePrefiltered        = "risk.recommended_scope.messages_prefiltered"
	meterRiskRecommendedScopeFindingsSuppressed = "risk.recommended_scope.findings_suppressed"
	meterRiskShadowMCPResolution                = "risk.shadow_mcp.resolution"
)

type riskMetrics struct {
	scanEvents                          metric.Int64Counter
	scanDuration                        metric.Float64Histogram
	ruleConfidence                      metric.Float64Histogram
	presidioScanSkipped                 metric.Int64Counter
	recommendedScopeMessagesPrefiltered metric.Int64Counter
	recommendedScopeFindingsSuppressed  metric.Int64Counter
	shadowMCPResolution                 metric.Int64Counter
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

	recommendedScopeMessagesPrefiltered, err := meter.Int64Counter(
		meterRiskRecommendedScopePrefiltered,
		metric.WithDescription("Messages skipped before expensive scanners by recommended category scopes"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskRecommendedScopePrefiltered), attr.SlogError(err))
	}

	recommendedScopeFindingsSuppressed, err := meter.Int64Counter(
		meterRiskRecommendedScopeFindingsSuppressed,
		metric.WithDescription("Findings suppressed by recommended category scopes"),
		metric.WithUnit("{finding}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskRecommendedScopeFindingsSuppressed), attr.SlogError(err))
	}

	shadowMCPResolution, err := meter.Int64Counter(
		meterRiskShadowMCPResolution,
		metric.WithDescription("MCP tool calls by how the shadow-MCP scanner resolved their server provenance"),
		metric.WithUnit("{tool_call}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRiskShadowMCPResolution), attr.SlogError(err))
	}

	return &riskMetrics{
		scanEvents:                          scanEvents,
		scanDuration:                        scanDuration,
		ruleConfidence:                      ruleConfidence,
		presidioScanSkipped:                 presidioScanSkipped,
		recommendedScopeMessagesPrefiltered: recommendedScopeMessagesPrefiltered,
		recommendedScopeFindingsSuppressed:  recommendedScopeFindingsSuppressed,
		shadowMCPResolution:                 shadowMCPResolution,
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

// RecordShadowMCPResolution counts one scanned MCP tool call by how its server
// provenance resolved. hookSource is the reporting agent, empty when the call
// had no provenance row at all; splitting on it is what makes the unresolved
// rate actionable per sender rather than a single opaque number, which is the
// signal for when the legacy signature fallback can be deleted.
func (m *riskMetrics) RecordShadowMCPResolution(ctx context.Context, orgID string, hookSource string, resolution string) {
	if m == nil || m.shadowMCPResolution == nil {
		return
	}
	m.shadowMCPResolution.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("gram.hook.source", conv.Default(hookSource, "unknown")),
		attribute.String("risk.shadow_mcp.resolution", resolution),
	))
}

func (m *riskMetrics) RecordRecommendedScopeSuppressed(ctx context.Context, orgID string, cat categories.Category) {
	if m == nil || m.recommendedScopeFindingsSuppressed == nil {
		return
	}
	m.recommendedScopeFindingsSuppressed.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("risk.category", string(cat)),
		attribute.Int("risk.recommended_scopes.version", recommendedscopes.Version),
	))
}
