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

	// ToolUnavailableReasonMetaKey is the per-tool _meta key that says why a
	// tool carrying [ToolUnavailableMetaKey] is unavailable. Its value is a
	// [ToolUnavailableReason].
	ToolUnavailableReasonMetaKey = "com.speakeasy/unavailable_reason"
)

// ToolUnavailableReason is the value of [ToolUnavailableReasonMetaKey].
type ToolUnavailableReason string

const (
	// ToolUnavailableReasonBlocked marks a tool an explicit block rule, such
	// as mcp:blocked_connect on the tool or its server, denies.
	ToolUnavailableReasonBlocked ToolUnavailableReason = "blocked"

	// ToolUnavailableReasonReadOnly marks a tool denied only by its
	// disposition where the caller's access covers read-only tools. A tool
	// denied by a grant limited to another disposition, such as idempotent,
	// reports [ToolUnavailableReasonNotGranted].
	ToolUnavailableReasonReadOnly ToolUnavailableReason = "read_only"

	// ToolUnavailableReasonNotGranted marks a tool no grant allows.
	ToolUnavailableReasonNotGranted ToolUnavailableReason = "not_granted"
)

// toolUnavailableReason maps an authz denial to its wire reason. A check
// that reports no denial was refused by the same evaluation moments earlier,
// so it falls back to not granted.
func toolUnavailableReason(reason authz.DenialReason) ToolUnavailableReason {
	switch reason {
	case authz.DenialReasonBlocked:
		return ToolUnavailableReasonBlocked
	case authz.DenialReasonReadOnly:
		return ToolUnavailableReasonReadOnly
	case authz.DenialReasonNone, authz.DenialReasonDisposition, authz.DenialReasonNotGranted:
		return ToolUnavailableReasonNotGranted
	default:
		return ToolUnavailableReasonNotGranted
	}
}

// toolsListInspection decides whether a tools/list response reports the whole
// upstream catalog with unusable tools marked, instead of dropping them. Only
// the listing changes: tools/call authorization still refuses a marked tool.
//
// It takes mcp:read or mcp:write on the server. mcp:read already exposes stored
// tool names, and mcp:write is checked separately because it survives a
// read block and is what lets a caller overwrite the stored catalog, which must
// always be written from the whole list. A consent selection is not subject to
// this: a session bound to one is never shown tools outside it.
type toolsListInspection struct {
	authz       *authz.Engine
	mcpServerID string
	projectID   string
}

// requested reports whether the listing asked to be inspected.
func (i toolsListInspection) requested(list *proxy.ToolsListResponse) bool {
	if i.authz == nil || list.Request == nil || list.Request.Params == nil {
		return false
	}
	requested, _ := list.Request.Params.Meta[ToolsListInspectMetaKey].(bool)
	return requested
}

// allowed reports whether the caller may inspect. It probes with
// [authz.Engine.Evaluate] because being refused only means an ordinary listing;
// recording a denial would ask admins to grant something nobody needed.
func (i toolsListInspection) allowed(ctx context.Context) (bool, error) {
	for _, scope := range []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPWrite} {
		ok, err := i.authz.Evaluate(ctx, authz.MCPCheck(scope, i.mcpServerID, i.projectID))
		if err != nil {
			return false, fmt.Errorf("authorize tools/list inspection: %w", err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// markToolUnavailable returns a copy of tool carrying [ToolUnavailableMetaKey]
// and [ToolUnavailableReasonMetaKey].
func markToolUnavailable(tool *mcp.Tool, reason ToolUnavailableReason) *mcp.Tool {
	marked := *tool
	marked.Meta = maps.Clone(tool.Meta)
	if marked.Meta == nil {
		marked.Meta = mcp.Meta{}
	}
	marked.Meta[ToolUnavailableMetaKey] = true
	marked.Meta[ToolUnavailableReasonMetaKey] = string(reason)
	return &marked
}

// ToolsListReservedMetaInterceptor removes [ToolUnavailableMetaKey] and
// [ToolUnavailableReasonMetaKey] from what an upstream sent, so the mark only
// ever means Speakeasy set it. It runs ahead of
// the access filter on private servers, the only ones that set the mark, because
// any interceptor on a public server would cost its listings the upstream's cache
// hints. A response without either key is left untouched.
type ToolsListReservedMetaInterceptor struct{}

var _ proxy.ToolsListResponseInterceptor = ToolsListReservedMetaInterceptor{}

// Name implements [proxy.ToolsListResponseInterceptor].
func (ToolsListReservedMetaInterceptor) Name() string {
	return "tools-list-reserved-meta"
}

// InterceptToolsListResponse implements [proxy.ToolsListResponseInterceptor].
func (ToolsListReservedMetaInterceptor) InterceptToolsListResponse(_ context.Context, list *proxy.ToolsListResponse) error {
	if list == nil || list.Result == nil {
		return nil
	}

	cleaned := make([]*mcp.Tool, 0, len(list.Result.Tools))
	found := false
	for _, tool := range list.Result.Tools {
		if tool == nil {
			cleaned = append(cleaned, tool)
			continue
		}
		_, marked := tool.Meta[ToolUnavailableMetaKey]
		_, reasoned := tool.Meta[ToolUnavailableReasonMetaKey]
		if !marked && !reasoned {
			cleaned = append(cleaned, tool)
			continue
		}
		found = true
		stripped := *tool
		stripped.Meta = maps.Clone(tool.Meta)
		delete(stripped.Meta, ToolUnavailableMetaKey)
		delete(stripped.Meta, ToolUnavailableReasonMetaKey)
		cleaned = append(cleaned, &stripped)
	}
	if !found {
		return nil
	}

	if err := list.SetTools(cleaned); err != nil {
		return fmt.Errorf("commit tools/list result without reserved meta: %w", err)
	}
	return nil
}
