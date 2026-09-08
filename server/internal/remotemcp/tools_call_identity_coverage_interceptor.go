package remotemcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

type identityCoverageCheckpoint interface {
	Record(context.Context, string, mcpmetrics.KillswitchCoverageSurface, mcptoolexecution.ServerSource)
}

// ToolsCallIdentityCoverageInterceptor records the private-proxy identity
// census before typed tools/call parameter decoding. The method-level hook
// covers malformed params that typed interceptors deliberately leave to the
// upstream MCP server for validation.
type ToolsCallIdentityCoverageInterceptor struct {
	checkpoint     identityCoverageCheckpoint
	identity       proxy.ServerIdentity
	organizationID string
}

var _ proxy.UserRequestInterceptor = (*ToolsCallIdentityCoverageInterceptor)(nil)

func NewToolsCallIdentityCoverageInterceptor(checkpoint identityCoverageCheckpoint, identity proxy.ServerIdentity, organizationID string) *ToolsCallIdentityCoverageInterceptor {
	return &ToolsCallIdentityCoverageInterceptor{
		checkpoint:     checkpoint,
		identity:       identity,
		organizationID: organizationID,
	}
}

func (i *ToolsCallIdentityCoverageInterceptor) Name() string {
	return "tools-call-identity-coverage"
}

// InterceptUserRequest records every parsed tools/call request and never
// rejects it. Validation and enforcement remain owned by their existing
// interceptors and the upstream MCP server.
func (i *ToolsCallIdentityCoverageInterceptor) InterceptUserRequest(ctx context.Context, req *proxy.UserRequest) error {
	if i == nil || i.checkpoint == nil || !proxy.IsToolsCallRequest(req) {
		return nil
	}

	frontingServerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if i.identity.McpServerID != "" {
		// A present but malformed route identity is a data-integrity failure,
		// not the deliberately unsupported legacy route with no server.
		frontingServerID.Valid = true
		frontingServerID.UUID, _ = uuid.Parse(i.identity.McpServerID)
	}
	i.checkpoint.Record(ctx, i.organizationID, mcpmetrics.KillswitchSurfacePrivateProxy, mcptoolexecution.ServerSource{
		FrontingServerID: frontingServerID,
	})
	return nil
}
