// Package riskscan observes mediated MCP operations without changing their outcomes.
package riskscan

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

// Event contains identifiers only, never arguments, credentials or response bodies.
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

	// ToolName is the execution name, empty for resources and prompts.
	ToolName string

	// ResourceURI identifies a resource read, empty for other operations.
	ResourceURI string

	// PromptName identifies a prompt render, empty for other operations.
	PromptName string

	// Phase identifies the point reached, not a policy decision.
	Phase string
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
	return &noop{tracer: provider.Tracer("github.com/speakeasy-api/gram/server/internal/riskscan")}
}

func (n *noop) Scan(ctx context.Context, event Event) {
	identity, stamped := mcpidentity.FromContext(ctx)
	_, span := n.tracer.Start(ctx, "risk.scan", trace.WithAttributes(
		attribute.String("gram.risk.scan.surface", event.Surface),
		attribute.String("gram.risk.scan.phase", event.Phase),
		attr.OrganizationID(event.OrganizationID),
		attr.ProjectID(event.ProjectID),
		attr.McpServerID(event.ServerID),
		attr.ToolsetID(event.ToolsetID),
		attr.ToolName(event.ToolName),
		attr.ResourceURI(event.ResourceURI),
		attribute.String("gram.risk.scan.prompt_name", event.PromptName),
		attribute.Bool("gram.risk.scan.identity_stamped", stamped),
		attribute.String("gram.risk.scan.principal_kind", string(identity.Kind())),
		attr.UserID(identity.UserID()),
	))
	span.End()
}
