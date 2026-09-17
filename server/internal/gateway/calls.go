package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// ScanEvaluator observes calls without changing their execution or results.
type ScanEvaluator interface {
	// Scan deliberately returns no error during observation; organization-wide
	// failure semantics are an AIS-688 contract. The payload reader is independent
	// of execution and may be nil.
	Scan(context.Context, io.Reader, mcpriskscan.Event)
}

// CallRoute identifies the entry route independently of the execution plan.
// Server and toolset membership belong to the route: the same plan can execute
// through multiple servers and toolsets, so neither can be derived from it.
type CallRoute struct {
	// Source overrides the proxy's default route source when nonempty.
	// Execution logs retain the proxy's configured source.
	Source ToolCallSource

	// ServerID identifies the server through which the call was routed.
	ServerID string

	// ToolsetID identifies the toolset through which the call was routed.
	ToolsetID string
}

// CallTool observes a mediated tool call before executing it. Payload borrows
// already-materialized request bytes independently of requestBody and must remain
// unchanged until the call returns. Stream-only callers may pass nil; this layer
// never materializes or consumes requestBody for observation.
func (tp *ToolProxy) CallTool(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	payload []byte,
	env toolconfig.ToolCallEnv,
	plan *ToolCallPlan,
	attrs tm.HTTPLogAttributes,
	route CallRoute,
) error {
	toolName := plan.Descriptor.Name
	if plan.Kind == ToolKindExternalMCP {
		toolName = plan.Descriptor.URN.Name
	}

	var input io.Reader
	if payload != nil {
		input = bytes.NewReader(payload)
	}
	tp.scanEvaluator.Scan(ctx, input, mcpriskscan.Event{
		Surface:        tp.scanSurface(route),
		Method:         mcpriskscan.MethodToolsCall,
		OrganizationID: plan.Descriptor.OrganizationID,
		ProjectID:      plan.Descriptor.ProjectID,
		ServerID:       route.ServerID,
		ToolsetID:      route.ToolsetID,
		ToolName:       toolName,
		ResourceURI:    "",
		PromptName:     "",
		Phase:          mcpriskscan.PhaseBeforeExecution,
	})

	return tp.Do(ctx, w, requestBody, env, plan, attrs)
}

// CallResource observes a mediated resource read before executing it.
func (tp *ToolProxy) CallResource(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	env toolconfig.ToolCallEnv,
	plan *ResourceCallPlan,
	attrs tm.HTTPLogAttributes,
	route CallRoute,
) error {
	// resources/read supplies a synthetic "{}" body, not caller arguments.
	tp.scanEvaluator.Scan(ctx, nil, mcpriskscan.Event{
		Surface:        tp.scanSurface(route),
		Method:         mcpriskscan.MethodResourcesRead,
		OrganizationID: plan.Descriptor.OrganizationID,
		ProjectID:      plan.Descriptor.ProjectID,
		ServerID:       route.ServerID,
		ToolsetID:      route.ToolsetID,
		ToolName:       "",
		ResourceURI:    plan.Descriptor.URI,
		PromptName:     "",
		Phase:          mcpriskscan.PhaseBeforeRead,
	})

	return tp.ReadResource(ctx, w, requestBody, env, plan, attrs)
}

func (tp *ToolProxy) scanSurface(route CallRoute) string {
	source := route.Source
	if source == "" {
		source = tp.source
	}
	switch source {
	case ToolCallSourceDirect:
		return mcpriskscan.SurfaceInstances
	case ToolCallSourceMCP:
		return mcpriskscan.SurfaceHostedMCP
	case ToolCallSourcePlatformMCP:
		return mcpriskscan.SurfacePlatformMCP
	default:
		return ""
	}
}
