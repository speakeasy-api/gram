// Package mcpriskscan observes mediated MCP operations without changing their outcomes.
package mcpriskscan

import (
	"context"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	SurfaceHostedMCP    = "hosted_mcp"
	SurfacePlatformMCP  = "platform_mcp"
	SurfaceInstances    = "instances"
	SurfaceResourceRead = "resources_read"
	SurfaceRemoteMCP    = "remote_mcp"
	SurfaceMetaMCP      = "meta_mcp"
	SurfacePromptsGet   = "prompts_get"

	PhaseBeforeExecution = "before_execution"
	PhaseBeforeRead      = "before_read"
	PhaseBeforeRender    = "before_render"
)

// Target carries route identity that a resolved execution plan does not contain.
type Target struct {
	// Surface identifies the serving route, not the underlying tool kind.
	Surface string

	// ServerID is the fronting mcp_servers row ID, empty when none exists.
	ServerID string

	// ToolsetID is the resolved toolset row ID, empty when none exists.
	ToolsetID string
}

// Event carries identifiers and caller-supplied request arguments, not transport
// credentials or resolved secrets. Response bodies are absent: every current seam
// runs before execution, reading, or rendering.
type Event struct {
	// Surface identifies the observed serving route.
	Surface string

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

	// Phase identifies the point reached, not a policy decision.
	Phase string

	// Payload borrows already-materialized request arguments: json.RawMessage for
	// tool calls or map[string]string for prompts. Evaluators must not mutate it.
	// Resource reads leave it nil because they have no meaningful request body.
	Payload any
}

// Evaluator is the MCP-scoped risk-policy evaluation seam for both record-only
// findings (flag policies, governance by record) and call gating (block policies).
type Evaluator interface {
	Scan(ctx context.Context, event Event)
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
		metric.WithDescription("MCP risk scans by serving surface and phase"),
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

func (n *noop) Scan(ctx context.Context, event Event) {
	start := time.Now()
	surface := attribute.String("gram.mcp.risk.scan.surface", event.Surface)
	phase := attribute.String("gram.mcp.risk.scan.phase", event.Phase)
	identity, stamped := mcpidentity.FromContext(ctx)
	// Deliberately select identifiers only. Payload can contain customer data;
	// never serialize the event or attach its payload to tracing.
	_, span := n.tracer.Start(ctx, "mcp.risk.scan", trace.WithAttributes(
		surface,
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

	// Count every scan, independent of trace sampling. Only the closed surface
	// and phase dimensions belong on metrics; identifiers and payloads do not.
	opts := metric.WithAttributes(surface, phase)
	if n.scans != nil {
		n.scans.Add(ctx, 1, opts)
	}
	if n.duration != nil {
		// The near-zero no-op duration is the instrumentation floor against
		// which real evaluation cost is measured, never upstream execution time.
		n.duration.Record(ctx, time.Since(start).Seconds(), opts)
	}
}
