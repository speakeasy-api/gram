// Package mcpriskscan evaluates mediated MCP operations before execution.
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
// Subject's single-owner, once-per-phase claim, owns the policy decision, and
// fans the claimed subject out to observation-only consumers.
type Evaluator struct {
	policy    *policyEvaluator
	observers []Observer
	metrics   scanMetrics
}

// NewEvaluator wraps observers with ownership and duplicate protection.
func NewEvaluator(observers ...Observer) *Evaluator {
	return &Evaluator{
		policy:    nil,
		observers: observers,
		metrics: scanMetrics{
			scans:       nil,
			duration:    nil,
			flagDropped: nil,
		},
	}
}

// PrependObserver composes test or instrumentation observation without
// exposing a second scan boundary that could bypass the subject claim.
func PrependObserver(observer Observer, next *Evaluator) *Evaluator {
	if next == nil {
		return NewEvaluator(observer)
	}
	clone := *next
	clone.observers = make([]Observer, 0, len(next.observers)+1)
	clone.observers = append(clone.observers, observer)
	clone.observers = append(clone.observers, next.observers...)
	return &clone
}

// Scan synchronously evaluates an authoritative subject at most once.
// Drain waits for detached flag evaluations after request admission has stopped.
func (e *Evaluator) Drain(ctx context.Context) error {
	if e == nil || e.policy == nil {
		return nil
	}
	return e.policy.drain(ctx)
}

func (e *Evaluator) Scan(ctx context.Context, subject Subject) Decision {
	if e == nil || !subject.claimEvaluation() {
		return Allow()
	}

	start := time.Now()
	decision := Allow()
	if e.policy != nil {
		decision = e.policy.evaluate(ctx, subject)
	}
	for _, observer := range e.observers {
		if observer != nil {
			observer.Observe(ctx, subject)
		}
	}
	e.metrics.record(ctx, subject.Event, decision.metricDecision(), time.Since(start))
	return decision
}

type scanMetrics struct {
	scans       metric.Int64Counter
	duration    metric.Float64Histogram
	flagDropped metric.Int64Counter
}

type noop struct {
	tracer trace.Tracer
}

// NewNoop returns an Evaluator that records reachability and the scan cost baseline,
// without evaluating policies, recording findings, or gating calls.
func NewNoop(tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *Evaluator {
	return newInstrumentedEvaluator(nil, tracerProvider, meterProvider, logger)
}

func newInstrumentedEvaluator(policy *policyEvaluator, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *Evaluator {
	const scope = "github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	meter := meterProvider.Meter(scope)
	scans, err := meter.Int64Counter(
		"mcp.risk.scan",
		metric.WithDescription("MCP risk scans by endpoint surface, MCP method, and decision"),
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
	flagDropped, err := meter.Int64Counter(
		"mcp.risk.scan.flag_dropped",
		metric.WithDescription("MCP flag-policy evaluations dropped before execution"),
		metric.WithUnit("{evaluation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName("mcp.risk.scan.flag_dropped"), attr.SlogError(err))
	}
	return &Evaluator{
		policy:    policy,
		observers: []Observer{&noop{tracer: tracerProvider.Tracer(scope)}},
		metrics: scanMetrics{
			scans:       scans,
			duration:    duration,
			flagDropped: flagDropped,
		},
	}
}

func (n *noop) Observe(ctx context.Context, subject Subject) {
	event := subject.Event
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
}

func (m scanMetrics) record(ctx context.Context, event Event, decision string, elapsed time.Duration) {
	surface := attribute.String("gram.mcp.risk.scan.surface", event.Surface)
	method := attribute.String("gram.mcp.risk.scan.method", event.Method)
	outcome := attribute.String("gram.mcp.risk.scan.decision", decision)
	opts := metric.WithAttributes(surface, method, outcome)
	if m.scans != nil {
		m.scans.Add(ctx, 1, opts)
	}
	if m.duration != nil {
		m.duration.Record(ctx, elapsed.Seconds(), opts)
	}
}

func (m scanMetrics) recordFlagDrop(ctx context.Context, event Event) {
	if m.flagDropped == nil {
		return
	}
	m.flagDropped.Add(ctx, 1, metric.WithAttributes(
		attribute.String("gram.mcp.risk.scan.surface", event.Surface),
		attribute.String("gram.mcp.risk.scan.method", event.Method),
	))
}
