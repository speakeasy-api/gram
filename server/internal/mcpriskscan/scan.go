// Package mcpriskscan observes mediated MCP operations without changing their outcomes.
package mcpriskscan

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"go.opentelemetry.io/otel/attribute"
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
	// tool calls or map[string]string for prompts. Hooks must not mutate it.
	// Resource reads leave it nil because they have no meaningful request body.
	Payload any
}

// Hook observes a seam without returning a decision or modifying its request.
type Hook interface {
	Scan(ctx context.Context, event Event)
}

type noop struct {
	tracer trace.Tracer
}

// NewNoop records reachability only; it performs no risk evaluation.
func NewNoop(provider trace.TracerProvider) Hook {
	return &noop{tracer: provider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpriskscan")}
}

func (n *noop) Scan(ctx context.Context, event Event) {
	identity, stamped := mcpidentity.FromContext(ctx)
	// Deliberately select identifiers only. Payload can contain customer data;
	// never serialize the event or attach its payload to tracing.
	_, span := n.tracer.Start(ctx, "mcp.risk.scan", trace.WithAttributes(
		attribute.String("gram.mcp.risk.scan.surface", event.Surface),
		attribute.String("gram.mcp.risk.scan.phase", event.Phase),
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
}
