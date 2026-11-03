package codemode

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func runtimeTracer(ctx context.Context) trace.Tracer {
	provider := otel.GetTracerProvider()
	if parent := trace.SpanFromContext(ctx); parent.SpanContext().IsValid() {
		provider = parent.TracerProvider()
	}
	return provider.Tracer("github.com/speakeasy-api/gram/server/internal/codemode")
}

func recordRuntimeOutcome(span trace.Span, err error) (codes.Code, string) {
	// Runtime errors can contain transport details. Record bounded outcomes,
	// never program text, tool arguments, credentials or raw error messages.
	outcome := "ok"
	switch {
	case err == nil:
	case errors.Is(err, errCodeSandboxPending):
		outcome = "pending"
	case errors.Is(err, context.Canceled):
		outcome = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		outcome = "timeout"
	case errors.Is(err, ErrCapacity), errors.Is(err, errRunnerFull):
		outcome = "capacity"
	case errors.Is(err, ErrRuntimeUnavailable):
		outcome = "unavailable"
	default:
		outcome = "error"
	}
	return recordOutcome(span, outcome)
}

func recordOutcome(span trace.Span, outcome string) (codes.Code, string) {
	span.SetAttributes(attr.CodeOutcome(outcome))
	if outcome != "ok" && outcome != "pending" {
		return codes.Error, outcome
	}
	return codes.Unset, ""
}

// ExecutionOutcome classifies runner results for runtime spans and gateway
// telemetry. Unknown codes remain a bounded error outcome; messages are omitted.
func ExecutionOutcome(failure *ExecutionError) string {
	if failure == nil {
		return "ok"
	}
	switch failure.Code {
	case "deadline_exceeded":
		return "timeout"
	case "cancelled":
		return "cancelled"
	case "capacity_exceeded":
		return "capacity"
	case "runtime_unavailable":
		return "unavailable"
	default:
		return "error"
	}
}
