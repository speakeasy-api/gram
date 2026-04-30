package shadowmcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/cache"
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

// XGramToolsetIDField is the JSON-schema property the MCP server injects
// into every Gram-hosted tool's input schema. Tool callers must echo this
// UUID back so the shadow-MCP validator can verify the call against its
// toolset.
const XGramToolsetIDField = "x-gram-toolset-id"

// ValidateGramToolsetCall enforces that a Gram-hosted tool call carries the
// required x-gram-toolset-id property, that the referenced toolset exists in
// the calling organization, and that the toolset contains a tool whose
// post-variation name matches toolName. Returns (reason, true) when the call
// fails validation; the reason is suitable for surfacing alongside a policy
// name in deny / flag messages.
//
// toolsetCache may be nil; the underlying mv.DescribeToolset call handles
// that path by skipping the cache entirely.
func ValidateGramToolsetCall(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolInput any,
	toolName string,
	orgID string,
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
	toolsetID, err := uuid.Parse(rawID)
	if err != nil {
		return fail(fmt.Sprintf("invalid %q value: not a UUID", XGramToolsetIDField))
	}

	toolsetRow, err := tsr.New(db).GetToolsetByIDAndOrganization(ctx, tsr.GetToolsetByIDAndOrganizationParams{
		ID:             toolsetID,
		OrganizationID: orgID,
	})
	if err != nil {
		return fail(fmt.Sprintf("toolset %s not found in this organization", toolsetID))
	}

	if toolName == "" {
		return fail("tool call missing tool name")
	}

	described, err := mv.DescribeToolset(
		ctx,
		logger,
		db,
		mv.ProjectID(toolsetRow.ProjectID),
		mv.ToolsetSlug(toolsetRow.Slug),
		toolsetCache,
	)
	if err != nil {
		return fail(fmt.Sprintf("failed to load toolset %s", toolsetID))
	}

	for _, tool := range described.Tools {
		base, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		if base.Name == toolName {
			return "", false
		}
	}

	return fail(fmt.Sprintf("tool %q is not part of toolset %s", toolName, toolsetID))
}
