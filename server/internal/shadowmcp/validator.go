package shadowmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// SourceShadowMCP is the policy source value that enables the shadow-MCP
// guard for a project. When at least one enabled risk policy carries this
// source, the MCP server injects the x-gram-toolset-id constant into tool
// schemas, and tool callers must echo a valid toolset id back. Block-action
// policies cause unsigned calls to be denied at the hook layer; flag-action
// policies surface as findings via the batch scanner.
const SourceShadowMCP = "shadow_mcp"

// SourceDestructiveTool is the policy source value that flags Gram MCP tool
// calls whose resolved tool definition has a destructive annotation.
const SourceDestructiveTool = "destructive_tool"

// XGramToolsetIDField is the JSON-schema property the MCP server injects
// into every Gram-hosted tool's input schema. Tool callers must echo this
// UUID back so the shadow-MCP validator can verify the call against its
// toolset.
const XGramToolsetIDField = "x-gram-toolset-id"

// DenyReason is the stable, kebab-case code identifying why a shadow-MCP
// validation denied a tool call. Emitted by ValidateToolsetCallReason and
// written verbatim as the rule_id on risk_results rows produced for
// shadow_mcp findings.
type DenyReason string

const (
	// DenyMissingToolsetID is returned when the recorded tool input does
	// not carry a usable x-gram-toolset-id property (absent, wrong type,
	// empty, or not a UUID).
	DenyMissingToolsetID DenyReason = "missing-toolset-id"
	// DenyUnknownToolset is returned when the referenced toolset cannot
	// be located in the calling organization (not found or load failure).
	DenyUnknownToolset DenyReason = "unknown-toolset"
	// DenyMissingToolName is returned when the recorded tool call has no
	// tool name to validate. Edge case; current callers filter empty tool
	// names before invoking the validator.
	DenyMissingToolName DenyReason = "missing-tool-name"
	// DenyToolNotInToolset is returned when the referenced toolset exists
	// but the named tool is not part of it.
	DenyToolNotInToolset DenyReason = "tool-not-in-toolset"
)

// ResolvedToolCall is a recorded MCP tool call resolved back to the Gram
// toolset and tool definition that produced it.
type ResolvedToolCall struct {
	ToolsetID string
	ToolName  string
	Tool      types.BaseToolAttributes
}

// ValidateToolsetCall enforces that a Gram-hosted tool call carries the
// required x-gram-toolset-id property, that the referenced toolset exists in
// the calling organization, and that the toolset contains a tool whose
// post-variation name matches toolName. Returns (detail, true) when the call
// fails validation; the detail is suitable for surfacing alongside a policy
// name in deny / flag messages on the hook path.
//
// Toolset lookups go through the Client's bundled toolset cache so callers
// on hot paths (tools/list hooks, batch scanner) share a single Redis-backed
// cache instance.
func (c *Client) ValidateToolsetCall(
	ctx context.Context,
	toolInput any,
	toolName string,
	orgID string,
) (string, bool) {
	_, _, detail, failed := c.resolveToolsetCall(ctx, toolInput, toolName, orgID)
	return detail, failed
}

// ValidateToolsetCallReason mirrors ValidateToolsetCall but additionally
// returns a stable DenyReason that identifies which validation rule
// rejected the call. The batch risk scanner uses the reason as the
// risk_results rule_id; the human-readable detail remains for logs.
func (c *Client) ValidateToolsetCallReason(
	ctx context.Context,
	toolInput any,
	toolName string,
	orgID string,
) (DenyReason, string, bool) {
	_, reason, detail, failed := c.resolveToolsetCall(ctx, toolInput, toolName, orgID)
	return reason, detail, failed
}

// ResolveToolsetCall resolves a recorded Gram MCP tool call to its underlying
// tool definition. It returns ok=false for missing provenance, unknown
// toolsets, and names that are not present in the resolved toolset.
func (c *Client) ResolveToolsetCall(
	ctx context.Context,
	toolInput any,
	toolName string,
	orgID string,
) (*ResolvedToolCall, bool) {
	resolved, _, _, failed := c.resolveToolsetCall(ctx, toolInput, toolName, orgID)
	return resolved, !failed
}

func (c *Client) resolveToolsetCall(
	ctx context.Context,
	toolInput any,
	toolName string,
	orgID string,
) (*ResolvedToolCall, DenyReason, string, bool) {
	inputMap, ok := toolInput.(map[string]any)
	if !ok {
		return nil, DenyMissingToolsetID, fmt.Sprintf("missing required %q property in tool input", XGramToolsetIDField), true
	}
	rawID, ok := inputMap[XGramToolsetIDField].(string)
	if !ok || rawID == "" {
		return nil, DenyMissingToolsetID, fmt.Sprintf("missing required %q property in tool input", XGramToolsetIDField), true
	}
	toolsetID, err := uuid.Parse(rawID)
	if err != nil {
		return nil, DenyMissingToolsetID, fmt.Sprintf("invalid %q value: not a UUID", XGramToolsetIDField), true
	}

	toolsetRow, err := tsr.New(c.db).GetToolsetByIDAndOrganization(ctx, tsr.GetToolsetByIDAndOrganizationParams{
		ID:             toolsetID,
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, DenyUnknownToolset, fmt.Sprintf("toolset %s not found in this organization", toolsetID), true
	}

	if toolName == "" {
		return nil, DenyMissingToolName, "tool call missing tool name", true
	}

	described, err := mv.DescribeToolset(
		ctx,
		c.logger,
		c.db,
		mv.ProjectID(toolsetRow.ProjectID),
		mv.ToolsetSlug(toolsetRow.Slug),
		&c.toolsetCache,
	)
	if err != nil {
		return nil, DenyUnknownToolset, fmt.Sprintf("failed to load toolset %s", toolsetID), true
	}

	for _, tool := range described.Tools {
		base, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		if base.Name == toolName {
			return &ResolvedToolCall{
				ToolsetID: toolsetID.String(),
				ToolName:  toolName,
				Tool:      base,
			}, "", "", false
		}
	}

	return nil, DenyToolNotInToolset, fmt.Sprintf("tool %q is not part of toolset %s", toolName, toolsetID), true
}
