// Package mcpriskscan observes mediated MCP operations without changing their outcomes.
package mcpriskscan

import (
	"context"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Observer performs one bounded synchronous inspection. It cannot return a
// decision, reject a request, or take ownership of borrowed payload bytes.
type Observer interface {
	Observe(ctx context.Context, subject Subject)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Subject)

// Observe implements Observer.
func (f ObserverFunc) Observe(ctx context.Context, subject Subject) {
	f(ctx, subject)
}

// Evaluator is the final scan boundary held by mediation seams. It enforces the
// Subject's single-owner, once-per-phase claim before invoking an Observer.
type Evaluator struct {
	observers []Observer
}

// NewEvaluator wraps observers with ownership and duplicate protection.
func NewEvaluator(observers ...Observer) *Evaluator {
	return &Evaluator{observers: observers}
}

// PrependObserver composes test or instrumentation observation without
// exposing a second scan boundary that could bypass the subject claim.
func PrependObserver(observer Observer, next *Evaluator) *Evaluator {
	if next == nil {
		return NewEvaluator(observer)
	}
	observers := make([]Observer, 0, len(next.observers)+1)
	observers = append(observers, observer)
	observers = append(observers, next.observers...)
	return NewEvaluator(observers...)
}

// Scan synchronously evaluates an authoritative subject at most once.
func (e *Evaluator) Scan(ctx context.Context, subject Subject) {
	if e == nil || !subject.claimEvaluation() {
		return
	}
	for _, observer := range e.observers {
		if observer != nil {
			observer.Observe(ctx, subject)
		}
	}
}

type noop struct {
	tracer   trace.Tracer
	scans    metric.Int64Counter
	duration metric.Float64Histogram
}

// NewNoop returns an Evaluator that records reachability and the scan cost baseline,
// without evaluating policies, recording findings, or gating calls.
func NewNoop(tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *Evaluator {
	const scope = "github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	meter := meterProvider.Meter(scope)
	scans, err := meter.Int64Counter(
		"mcp.risk.scan",
		metric.WithDescription("MCP risk scans by endpoint surface and MCP method"),
		metric.WithUnit("{scan}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName("mcp.risk.scan"), attr.SlogError(err))
	}
	duration, err := meter.Float64Histogram(
		"mcp.risk.scan.duration",
		metric.WithDescription("Duration of an MCP risk scan in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.000001, 0.00001, 0.0001, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName("mcp.risk.scan.duration"), attr.SlogError(err))
	}
	return NewEvaluator(&noop{
		tracer:   tracerProvider.Tracer(scope),
		scans:    scans,
		duration: duration,
	})
}

func (n *noop) Observe(ctx context.Context, subject Subject) {
	event := subject.Event
	start := time.Now()
	surface := attribute.String("gram.mcp.risk.scan.surface", event.Surface)
	method := attribute.String("gram.mcp.risk.scan.method", event.Method)
	phase := attribute.String("gram.mcp.risk.scan.phase", event.Phase())
	// Deliberately select Event metadata only. Never read or attach Subject.Payload.
	_, span := n.tracer.Start(ctx, "mcp.risk.scan", trace.WithAttributes(
		surface,
		method,
		phase,
		attr.OrganizationID(event.OrganizationID),
		attr.ProjectID(event.ProjectID),
		attr.McpServerID(event.ServerID),
		attr.ToolsetID(event.ToolsetID),
		attr.ToolName(event.ToolName),
		attr.ResourceURI(event.ResourceURI),
		attribute.String("gram.mcp.risk.scan.prompt_name", event.PromptName),
		attribute.String("gram.mcp.risk.scan.execution_id", event.ExecutionID()),
		attribute.String("gram.mcp.risk.scan.meta_mcp_server_id", event.MetaServerID),
		attribute.Bool("gram.mcp.risk.scan.evaluation_owner", subject.EvaluationOwner()),
		attribute.Bool("gram.mcp.risk.scan.identity_stamped", event.IdentityStamped()),
		attribute.String("gram.mcp.risk.scan.principal_kind", string(event.Principal().Kind())),
		attr.UserID(event.Principal().UserID()),
	))
	span.End()

	// Count every scan, independent of trace sampling. Only the closed endpoint
	// surface and MCP method dimensions belong on metrics; phase and identifiers do not.
	opts := metric.WithAttributes(surface, method)
	if n.scans != nil {
		n.scans.Add(ctx, 1, opts)
	}
	if n.duration != nil {
		// The near-zero no-op duration is the instrumentation floor against
		// which real evaluation cost is measured, never upstream execution time.
		n.duration.Record(ctx, time.Since(start).Seconds(), opts)
	}
}
