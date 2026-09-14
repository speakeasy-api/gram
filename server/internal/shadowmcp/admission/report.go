package admission

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// ReportOutcome is a bounded, non-identifying observation of report-mode admission.
type ReportOutcome string

const (
	// ReportEmptyAudience means no recipients would receive the MCP.
	ReportEmptyAudience ReportOutcome = "empty_audience"
	// ReportNotRequired means no blocking policy requires approval.
	ReportNotRequired ReportOutcome = "not_required"
	// ReportCovered means standing approval covers the complete audience.
	ReportCovered ReportOutcome = "covered"
	// ReportApprovalRequired means the complete audience lacks standing approval.
	ReportApprovalRequired ReportOutcome = "approval_required"
	// ReportInvalidTarget means the proposed URL cannot be canonicalized.
	ReportInvalidTarget ReportOutcome = "invalid_target"
	// ReportUnavailable means admission could not reach a trustworthy decision.
	ReportUnavailable ReportOutcome = "unavailable"
)

// ReportObserver records evaluations without exposing URLs or audience identities.
type ReportObserver interface {
	RecordReport(context.Context, ReportOutcome)
}

const reportEvaluationsMetric = "shadow_mcp.distribution_admission.report_evaluations"
const reportOutcomeAttribute = "shadow_mcp.distribution_admission.outcome"

// ReportMetrics counts report-mode evaluations by bounded outcome only.
type ReportMetrics struct {
	evaluations metric.Int64Counter
}

func NewReportMetrics(provider metric.MeterProvider, logger *slog.Logger) *ReportMetrics {
	meter := provider.Meter("github.com/speakeasy-api/gram/server/internal/shadowmcp/admission")
	counter, err := meter.Int64Counter(reportEvaluationsMetric,
		metric.WithDescription("Shadow MCP distribution report-mode evaluations by outcome"),
		metric.WithUnit("{evaluation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create distribution admission report metric", attr.SlogMetricName(reportEvaluationsMetric), attr.SlogError(err))
	}
	return &ReportMetrics{evaluations: counter}
}

func (m *ReportMetrics) RecordReport(ctx context.Context, outcome ReportOutcome) {
	if m == nil || m.evaluations == nil {
		return
	}
	switch outcome {
	case ReportEmptyAudience, ReportNotRequired, ReportCovered, ReportApprovalRequired, ReportInvalidTarget, ReportUnavailable:
	default:
		outcome = ReportUnavailable
	}
	m.evaluations.Add(ctx, 1, metric.WithAttributes(attribute.String(reportOutcomeAttribute, string(outcome))))
}
