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
	span.SetAttributes(attr.CodeOutcome(outcome))
	if err != nil && outcome != "pending" {
		return codes.Error, outcome
	}
	return codes.Unset, ""
}

// runtimeExecutionError maps untrusted runner codes to the same bounded
// categories as acquisition and transport errors, without recording the message.
func runtimeExecutionError(failure *ExecutionError) error {
	if failure == nil {
		return nil
	}
	switch failure.Code {
	case "deadline_exceeded":
		return context.DeadlineExceeded
	case "cancelled":
		return context.Canceled
	case "capacity_exceeded":
		return ErrCapacity
	case "runtime_unavailable":
		return ErrRuntimeUnavailable
	default:
		return errors.New("execution failed")
	}
}
