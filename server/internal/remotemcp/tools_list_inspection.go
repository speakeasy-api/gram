package remotemcp

import (
	"context"
	"fmt"
	"maps"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const (
	// ToolsListInspectMetaKey is the tools/list request _meta key a management
	// client sets to receive the upstream's whole catalog rather than the
	// caller's view of it.
	ToolsListInspectMetaKey = "com.speakeasy/inspect"

	// ToolUnavailableMetaKey is the per-tool _meta key set on a tool that an
	// inspected tools/list kept even though this caller cannot use it. Tools
	// without the key are available to the caller.
	ToolUnavailableMetaKey = "com.speakeasy/unavailable"
)

// toolsListInspection decides whether a tools/list response reports the whole
// upstream catalog with unusable tools marked, instead of dropping them. Only
// the listing changes: tools/call authorization still refuses a marked tool.
//
// It takes mcp:read or mcp:write on the server. mcp:read already exposes stored
// tool names, and mcp:write is checked separately because it survives a
// read block and is what lets a caller overwrite the stored catalog, which must
// always be written from the whole list.
type toolsListInspection struct {
	authz       *authz.Engine
	mcpServerID string
	projectID   string
}

func (i *toolsListInspection) enabled(ctx context.Context, list *proxy.ToolsListResponse) (bool, error) {
	if i == nil || i.authz == nil || list.Request == nil || list.Request.Params == nil {
		return false, nil
	}
	if requested, _ := list.Request.Params.Meta[ToolsListInspectMetaKey].(bool); !requested {
		return false, nil
	}

	matched, err := i.authz.FindMatched(ctx, []authz.Check{
		authz.MCPCheck(authz.ScopeMCPRead, i.mcpServerID, i.projectID),
		authz.MCPCheck(authz.ScopeMCPWrite, i.mcpServerID, i.projectID),
	})
	if err != nil {
		return false, fmt.Errorf("authorize tools/list inspection: %w", err)
	}
	for _, ok := range matched {
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// markToolUnavailable returns a copy of tool carrying [ToolUnavailableMetaKey].
func markToolUnavailable(tool *mcp.Tool) *mcp.Tool {
	marked := *tool
	marked.Meta = maps.Clone(tool.Meta)
	if marked.Meta == nil {
		marked.Meta = mcp.Meta{}
	}
	marked.Meta[ToolUnavailableMetaKey] = true
	return &marked
}
