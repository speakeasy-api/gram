package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// toolAuthzEnforced reports whether a hosted request is subject to per-tool
// mcp:connect checks: an authenticated caller of a private MCP. Public MCPs
// are open to everyone, mirroring the connection-level guard. Both the
// privacy read and the resource id follow the wrapper when one fronts the
// request.
func toolAuthzEnforced(authzEngine *authz.Engine, payload *mcpInputs, toolset *types.Toolset) bool {
	return payload.authenticated && authzEngine != nil && payload.effectiveMCPPrivate(toolset.McpIsPublic)
}

// toolAllowed runs the per-tool mcp:connect check for one concrete tool, with
// the same dimensions tools/call uses. A denial is reported as false; any
// other failure (missing grants, a database error) is returned so the request
// fails rather than reading as "no tools".
func toolAllowed(ctx context.Context, authzEngine *authz.Engine, payload *mcpInputs, toolset *types.Toolset, name, disposition string) (bool, error) {
	err := authzEngine.Require(ctx, authz.MCPToolCallCheck(payload.mcpConnectResourceID(toolset.ID), authz.MCPToolCallDimensions{
		Tool:        name,
		Disposition: disposition,
		ProjectID:   payload.projectID.String(),
	}))
	if err == nil {
		return true, nil
	}

	var oopsErr *oops.ShareableError
	if errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeForbidden {
		return false, nil
	}
	return false, fmt.Errorf("check tool-level authz: %w", err)
}

// authorizedDiscoveryToolset is the toolset dynamic discovery (search_tools,
// describe_tools and the facade's own examples) may reveal to this caller.
// Without per-tool enforcement it is the toolset itself. With it, it is a
// request-local copy without the materialized tools the caller may not call,
// each checked by its effective name and annotations exactly as tools/call
// checks it. External MCP proxy placeholders are kept as they are, so dynamic
// mode treats them as it always has, but they never count as a tool the
// caller may call. allowed is the number of tools the caller may call, and
// restricted reports whether any tool was withheld.
func authorizedDiscoveryToolset(ctx context.Context, authzEngine *authz.Engine, payload *mcpInputs, toolset *types.Toolset) (discovery *types.Toolset, allowed int, restricted bool, err error) {
	if !toolAuthzEnforced(authzEngine, payload, toolset) {
		return toolset, len(toolset.Tools), false, nil
	}

	kept := make([]*types.Tool, 0, len(toolset.Tools))
	for _, tool := range toolset.Tools {
		if conv.IsProxyTool(tool) {
			kept = append(kept, tool)
			continue
		}
		baseTool, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		ok, err := toolAllowed(ctx, authzEngine, payload, toolset, baseTool.Name, conv.DispositionFromAnnotations(baseTool.Annotations))
		if err != nil {
			return nil, 0, false, err
		}
		if ok {
			kept = append(kept, tool)
			allowed++
		}
	}

	scoped := *toolset
	scoped.Tools = kept
	return &scoped, allowed, len(kept) != len(toolset.Tools), nil
}
