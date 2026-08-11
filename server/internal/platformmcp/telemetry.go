package platformmcp

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const platformMCPEventMetric = "platform_mcp.lifecycle.events"

type LifecycleEvent struct {
	Operation string
	Phase     string
	Outcome   string
	State     ReadinessState
}

type LifecycleTelemetry interface {
	Record(ctx context.Context, event LifecycleEvent)
}

type noopLifecycleTelemetry struct{}

func (noopLifecycleTelemetry) Record(context.Context, LifecycleEvent) {}

type lifecycleTelemetry struct {
	logger  *slog.Logger
	counter metric.Int64Counter
}

func NewLifecycleTelemetry(logger *slog.Logger, meterProvider metric.MeterProvider) LifecycleTelemetry {
	if logger == nil || meterProvider == nil {
		return noopLifecycleTelemetry{}
	}
	counter, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/platformmcp").Int64Counter(
		platformMCPEventMetric,
		metric.WithDescription("Bounded Platform MCP lifecycle events"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create Platform MCP lifecycle metric", attr.SlogError(err))
	}
	return &lifecycleTelemetry{logger: logger, counter: counter}
}

func (t *lifecycleTelemetry) Record(ctx context.Context, event LifecycleEvent) {
	if !validLifecycleEvent(event) {
		return
	}
	attributes := []attribute.KeyValue{
		attribute.String("platform_mcp.operation", event.Operation),
		attribute.String("platform_mcp.phase", event.Phase),
		attribute.String("platform_mcp.outcome", event.Outcome),
	}
	if event.State != "" {
		attributes = append(attributes, attribute.String("platform_mcp.readiness_state", string(event.State)))
	}
	if t.counter != nil {
		t.counter.Add(ctx, 1, metric.WithAttributes(attributes...))
	}
	t.logger.LogAttrs(ctx, slog.LevelInfo, "platform mcp lifecycle event",
		attr.SlogEvent("platform_mcp.lifecycle"),
		attr.SlogName(event.Operation),
		attr.SlogValueString(event.Phase),
		attr.SlogReason(event.Outcome),
		attr.SlogExpected(string(event.State)),
	)
}

func lifecycleOutcome(err error) string {
	switch {
	case err == nil:
		return "succeeded"
	case errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrReadinessRateLimited):
		return "rate_limited"
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrUnauthorized), errors.Is(err, ErrTargetIneligible):
		return "denied"
	case errors.Is(err, ErrSetupHandoffInvalid), errors.Is(err, ErrRegistrationInvalid), errors.Is(err, ErrReadinessInvalid):
		return "invalid"
	default:
		return "unavailable"
	}
}

func validLifecycleEvent(event LifecycleEvent) bool {
	if event.Operation != "registration" && event.Operation != "provider_setup" && event.Operation != "readiness" {
		return false
	}
	if !validLifecyclePhase(event.Operation, event.Phase) || !validLifecycleOutcome(event.Outcome) {
		return false
	}
	return event.State == "" || isReadinessState(event.State)
}

func validLifecyclePhase(operation, phase string) bool {
	switch operation {
	case "registration":
		return phase == "complete"
	case "provider_setup":
		return phase == "handoff"
	case "readiness":
		return phase == "forced_probe"
	default:
		return false
	}
}

func validLifecycleOutcome(outcome string) bool {
	switch outcome {
	case "succeeded", "rate_limited", "denied", "invalid", "unavailable":
		return true
	default:
		return false
	}
}
