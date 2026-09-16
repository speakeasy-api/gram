package temporal

import (
	"context"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/workflow"
)

// StartTracingSpan keeps normal workflow rollovers out of tracing errors.
// The Temporal OpenTelemetry adapter records the returned error before setting
// the span status, allowing us to recognize the typed ContinueAsNew sentinel.
func StartTracingSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) trace.Span {
	_, span := tracer.Start(ctx, name, opts...)
	if !strings.HasPrefix(name, "RunWorkflow:") {
		return span
	}
	return &workflowTracingSpan{Span: span, mu: sync.Mutex{}, rollover: ""}
}

type workflowTracingSpan struct {
	trace.Span
	mu       sync.Mutex
	rollover string
}

func (s *workflowTracingSpan) RecordError(err error, opts ...trace.EventOption) {
	if workflow.IsContinueAsNewError(err) {
		s.mu.Lock()
		s.rollover = err.Error()
		s.mu.Unlock()
		return
	}
	s.Span.RecordError(err, opts...)
}

func (s *workflowTracingSpan) SetStatus(code codes.Code, description string) {
	s.mu.Lock()
	rollover := s.rollover
	s.mu.Unlock()
	if code == codes.Error && rollover != "" && description == rollover {
		return
	}
	s.Span.SetStatus(code, description)
}
