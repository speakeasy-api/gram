// Package mcpriskscan observes mediated MCP operations without changing their outcomes.
package mcpriskscan

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	SurfaceHostedMCP   = "hosted_mcp"
	SurfacePlatformMCP = "platform_mcp"
	SurfaceInstances   = "instances"
	SurfaceRemoteMCP   = "remote_mcp"

	MethodToolsCall     = "tools/call"
	MethodResourcesRead = "resources/read"
	MethodPromptsGet    = "prompts/get"

	PhaseBeforeExecution = "before_execution"
	PhaseBeforeRead      = "before_read"
	PhaseBeforeRender    = "before_render"
)

// Event carries operation and target metadata, excluding credentials and payloads.
// Response bodies are absent: every current seam runs before execution, reading,
// or rendering.
type Event struct {
	// Surface identifies the observed serving route.
	Surface string

	// Method identifies the MCP operation, including equivalent direct tool invocations.
	Method string

	// OrganizationID identifies the organization owning the target.
	OrganizationID string

	// ProjectID identifies the project owning the target.
	ProjectID string

	// ServerID is the fronting mcp_servers row ID, empty when none exists.
	ServerID string

	// ToolsetID is the resolved toolset row ID, empty when unavailable.
	ToolsetID string

	// ToolName is the resolved name, or stable proxy URN name for external MCP; empty for resources and prompts.
	ToolName string

	// ResourceURI identifies a resource read, empty for other operations.
	ResourceURI string

	// PromptName identifies a prompt render, empty for other operations.
	PromptName string

	// Phase identifies the point reached for tracing, not a metric dimension or policy decision.
	Phase string
}

// Evaluator is the MCP-scoped risk-policy evaluation seam for both record-only
// findings (flag policies, governance by record) and call gating (block policies).
//
// Scan deliberately returns no error during the observation-only phase, so an
// observation cannot alter an outcome. Enforcement decisions and organization-wide
// fail-open/fail-closed semantics are an AIS-688 contract, not a per-caller choice.
// Payload readers borrow existing argument bytes independently of execution and
// may be nil when a seam has no materialized argument bytes.
type Evaluator interface {
	Scan(ctx context.Context, payload io.Reader, event Event)
}

type noop struct {
	tracer   trace.Tracer
	scans    metric.Int64Counter
	duration metric.Float64Histogram
}

// NewNoop returns an Evaluator that records reachability and the scan cost baseline,
// without evaluating policies, recording findings, or gating calls.
func NewNoop(tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) Evaluator {
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
	return &noop{
		tracer:   tracerProvider.Tracer(scope),
		scans:    scans,
		duration: duration,
	}
}

func (n *noop) Scan(ctx context.Context, _ io.Reader, event Event) {
	start := time.Now()
	surface := attribute.String("gram.mcp.risk.scan.surface", event.Surface)
	method := attribute.String("gram.mcp.risk.scan.method", event.Method)
	phase := attribute.String("gram.mcp.risk.scan.phase", event.Phase)
	identity, stamped := mcpidentity.FromContext(ctx)
	// Deliberately ignore the payload reader and select identifiers only.
	// Never read or attach customer payloads to tracing.
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
		attribute.Bool("gram.mcp.risk.scan.identity_stamped", stamped),
		attribute.String("gram.mcp.risk.scan.principal_kind", string(identity.Kind())),
		attr.UserID(identity.UserID()),
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
