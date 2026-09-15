package platformmcp

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
)

const (
	platformMCPRiskToolCallMetric     = "platform_mcp.risk.tool_calls"
	platformMCPRiskToolDurationMetric = "platform_mcp.risk.tool_duration"

	riskTelemetryNotApplicable = "not_applicable"
	riskTelemetryFresh         = "fresh"
	riskTelemetryReceiptReplay = "receipt_replay"
	riskTelemetryMatched       = "matched_existing"
	riskTelemetryScheduled     = "scheduled"
)

type RiskToolEvent struct {
	Tool           string
	Outcome        string
	Replay         string
	CatalogVersion string
	Reconciliation string
}

type RiskTelemetry interface {
	Record(context.Context, RiskToolEvent, time.Duration)
}

type noopRiskTelemetry struct{}

func (noopRiskTelemetry) Record(context.Context, RiskToolEvent, time.Duration) {}

type riskTelemetry struct {
	calls    metric.Int64Counter
	duration metric.Float64Histogram
}

func NewRiskTelemetry(logger *slog.Logger, meterProvider metric.MeterProvider) RiskTelemetry {
	if logger == nil || meterProvider == nil {
		return noopRiskTelemetry{}
	}

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/platformmcp")
	calls, err := meter.Int64Counter(platformMCPRiskToolCallMetric, metric.WithDescription("Bounded Platform MCP risk tool outcomes"), metric.WithUnit("{call}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP risk metric", attr.SlogMetricName(platformMCPRiskToolCallMetric), attr.SlogError(err))
	}
	duration, err := meter.Float64Histogram(platformMCPRiskToolDurationMetric, metric.WithDescription("Platform MCP risk tool handler duration"), metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP risk metric", attr.SlogMetricName(platformMCPRiskToolDurationMetric), attr.SlogError(err))
	}
	return &riskTelemetry{calls: calls, duration: duration}
}

func (t *riskTelemetry) Record(ctx context.Context, event RiskToolEvent, duration time.Duration) {
	if t == nil || !validRiskToolEvent(event) || duration < 0 {
		return
	}
	attributes := metric.WithAttributes(
		attribute.String("platform_mcp.risk.tool", event.Tool),
		attribute.String("platform_mcp.risk.outcome", event.Outcome),
		attribute.String("platform_mcp.risk.replay", event.Replay),
		attribute.String("platform_mcp.risk.catalog_version", event.CatalogVersion),
		attribute.String("platform_mcp.risk.reconciliation", event.Reconciliation),
	)
	if t.calls != nil {
		t.calls.Add(ctx, 1, attributes)
	}
	if t.duration != nil {
		t.duration.Record(ctx, duration.Seconds(), attributes)
	}
}

func validRiskToolEvent(event RiskToolEvent) bool {
	return validRiskTelemetryTool(event.Tool) &&
		validRiskTelemetryOutcome(event.Outcome) &&
		validRiskTelemetryReplay(event.Replay) &&
		event.CatalogVersion == policycatalog.SchemaV1 &&
		validRiskTelemetryReconciliation(event.Reconciliation)
}

func validRiskTelemetryTool(tool string) bool {
	switch tool {
	case "list_risk_policies", "get_risk_policy", "create_risk_policy", "update_risk_policy", "list_risk_exclusions", "create_risk_exclusion", "update_risk_exclusion":
		return true
	default:
		return false
	}
}

func validRiskTelemetryOutcome(outcome string) bool {
	switch outcome {
	case "succeeded", unavailableCode, "invalid_request", "not_found", "conflict", "rate_limited", "repair_required", "unavailable":
		return true
	default:
		return false
	}
}

func validRiskTelemetryReplay(replay string) bool {
	switch replay {
	case riskTelemetryNotApplicable, riskTelemetryFresh, riskTelemetryReceiptReplay, riskTelemetryMatched:
		return true
	default:
		return false
	}
}

func validRiskTelemetryReconciliation(state string) bool {
	return state == riskTelemetryNotApplicable || state == riskTelemetryScheduled
}

func riskTelemetryEvent(tool, outcome string) RiskToolEvent {
	return RiskToolEvent{
		Tool:           tool,
		Outcome:        outcome,
		Replay:         riskTelemetryNotApplicable,
		CatalogVersion: policycatalog.SchemaV1,
		Reconciliation: riskTelemetryNotApplicable,
	}
}
