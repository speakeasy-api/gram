package shadowmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
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
// post-variation name matches toolName. Returns (reason, true) when the call
// fails validation; the reason is suitable for surfacing alongside a policy
// name in deny / flag messages.
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
	_, detail, failed := c.resolveToolsetCall(ctx, toolInput, toolName, orgID)
	return detail, failed
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
	resolved, _, failed := c.resolveToolsetCall(ctx, toolInput, toolName, orgID)
	return resolved, !failed
}

func (c *Client) resolveToolsetCall(
	ctx context.Context,
	toolInput any,
	toolName string,
	orgID string,
) (*ResolvedToolCall, string, bool) {
	fail := func(detail string) (string, bool) {
		return detail, true
	}

	inputMap, ok := toolInput.(map[string]any)
	if !ok {
		detail, failed := fail(fmt.Sprintf("missing required %q property in tool input", XGramToolsetIDField))
		return nil, detail, failed
	}
	rawID, ok := inputMap[XGramToolsetIDField].(string)
	if !ok || rawID == "" {
		detail, failed := fail(fmt.Sprintf("missing required %q property in tool input", XGramToolsetIDField))
		return nil, detail, failed
	}
	toolsetID, err := uuid.Parse(rawID)
	if err != nil {
		detail, failed := fail(fmt.Sprintf("invalid %q value: not a UUID", XGramToolsetIDField))
		return nil, detail, failed
	}

	toolsetRow, err := tsr.New(c.db).GetToolsetByIDAndOrganization(ctx, tsr.GetToolsetByIDAndOrganizationParams{
		ID:             toolsetID,
		OrganizationID: orgID,
	})
	if err != nil {
		detail, failed := fail(fmt.Sprintf("toolset %s not found in this organization", toolsetID))
		return nil, detail, failed
	}

	if toolName == "" {
		detail, failed := fail("tool call missing tool name")
		return nil, detail, failed
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
		detail, failed := fail(fmt.Sprintf("failed to load toolset %s", toolsetID))
		return nil, detail, failed
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
			}, "", false
		}
	}

	detail, failed := fail(fmt.Sprintf("tool %q is not part of toolset %s", toolName, toolsetID))
	return nil, detail, failed
}

// ValidateRemoteMCPServerCall is the `/x/mcp` analogue of
// [Client.ValidateToolsetCall]. It enforces that a tool call against a
// remote MCP server proxied by Gram carries the required
// [XGramToolsetIDField] property and that the echoed UUID resolves to a
// remote_mcp_server in the calling project. Returns (reason, true) when
// the call fails validation; the reason is suitable for surfacing
// alongside a policy name in deny / flag messages.
//
// The property name [XGramToolsetIDField] is shared with the toolset
// path for parity with downstream client-side hooks (Cursor, Claude
// Code), which validate the same property regardless of whether the
// backing scope is a Gram toolset or a remote MCP server.
//
// Tool name presence is required but is not cross-checked against a
// curated catalog: remote MCP servers expose their tool catalog
// dynamically and Gram does not mirror it. Risk policies that target
// specific (server, tool) combinations enforce that constraint
// separately at the policy-evaluation layer.
//
// A Resolve-shaped helper is intentionally not exposed for this path —
// remote MCP servers carry no Gram-side BaseToolAttributes for
// downstream policy evaluation to consume. Add one when a concrete
// consumer needs the resolved view.
func (c *Client) ValidateRemoteMCPServerCall(
	ctx context.Context,
	toolInput any,
	toolName string,
	projectID string,
) (string, bool) {
	fail := func(detail string) (string, bool) {
		return detail, true
	}

	inputMap, ok := toolInput.(map[string]any)
	if !ok {
		return fail(fmt.Sprintf("missing required %q property in tool input", XGramToolsetIDField))
	}
	rawID, ok := inputMap[XGramToolsetIDField].(string)
	if !ok || rawID == "" {
		return fail(fmt.Sprintf("missing required %q property in tool input", XGramToolsetIDField))
	}
	serverID, err := uuid.Parse(rawID)
	if err != nil {
		return fail(fmt.Sprintf("invalid %q value: not a UUID", XGramToolsetIDField))
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fail("invalid project id")
	}

	if _, err := remotemcprepo.New(c.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: projectUUID,
	}); err != nil {
		return fail(fmt.Sprintf("remote mcp server %s not found in this project", serverID))
	}

	if toolName == "" {
		return fail("tool call missing tool name")
	}

	return "", false
}
