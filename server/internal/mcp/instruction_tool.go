package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
)

// The synthetic instruction tool surfaces mcp_metadata.instructions as a
// callable tool on every Gram MCP server, because many MCP clients ignore the
// instructions field of the initialize response. In "required" mode (the
// default) tools/call gates each MCP session on reading the instructions
// before any other tool runs.
const instructionsToolName = "instructions"

const instructionsToolDescription = "Server usage guide for this MCP server. Returns organization-specific conventions, required workflows, and verification steps for using the other tools. Call this once before using any other tool."

const instructionsNotConfiguredMessage = "No instructions have been configured for this server. An administrator can add them in the Gram dashboard under Server Instructions."

type instructionToolMode string

const (
	instructionToolModeDisabled instructionToolMode = "disabled"
	instructionToolModeOptional instructionToolMode = "optional"
	instructionToolModeRequired instructionToolMode = "required"
)

// parseInstructionToolMode maps a stored mode value to a known mode. Empty
// (legacy rows written before the column's application-side default) and
// unknown values fail safe to required.
func parseInstructionToolMode(raw string) instructionToolMode {
	switch instructionToolMode(raw) {
	case instructionToolModeDisabled, instructionToolModeOptional:
		return instructionToolMode(raw)
	default:
		return instructionToolModeRequired
	}
}

type instructionToolConfig struct {
	Mode         instructionToolMode
	Instructions string
}

// fetchInstructionToolConfig loads the instruction tool settings for a
// toolset. Lookup failures fail open: the tool stays listed (mode required)
// but with no instructions content, which also keeps the gate disarmed.
func fetchInstructionToolConfig(ctx context.Context, logger *slog.Logger, metadataRepo *mcpmetadata_repo.Queries, toolsetID uuid.UUID) instructionToolConfig {
	fallback := instructionToolConfig{Mode: instructionToolModeRequired, Instructions: ""}

	metadata, err := metadataRepo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: toolsetID, Valid: true})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch MCP metadata for instruction tool", attr.SlogError(err))
		}
		return fallback
	}

	cfg := instructionToolConfig{
		Mode:         parseInstructionToolMode(metadata.InstructionToolMode),
		Instructions: "",
	}
	if metadata.Instructions.Valid {
		cfg.Instructions = metadata.Instructions.String
	}
	return cfg
}

var instructionToolInputSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

func buildInstructionToolEntry() *toolListEntry {
	readOnly := true
	idempotent := true
	return &toolListEntry{
		Name:        instructionsToolName,
		Description: instructionsToolDescription,
		InputSchema: instructionToolInputSchema,
		Annotations: &externalmcp.ToolAnnotations{
			ReadOnlyHint:   &readOnly,
			IdempotentHint: &idempotent,
		},
		Meta: nil,
	}
}

// injectInstructionTool prepends the synthetic instruction tool to a built
// tools/list, unless the mode disables it or a real tool already claims the
// name (Gram never shadows a customer tool). entries covers tools already
// materialized for the response (including live-listed proxy tools);
// toolsetTools additionally covers underlying tools not present in entries,
// e.g. real tools reachable via execute_tool in dynamic mode.
func injectInstructionTool(entries []*toolListEntry, toolsetTools []*types.Tool, mode instructionToolMode) []*toolListEntry {
	if mode == instructionToolModeDisabled {
		return entries
	}
	for _, e := range entries {
		if e.Name == instructionsToolName {
			return entries
		}
	}
	if toolsetExposesInstructionsTool(toolsetTools) {
		return entries
	}
	return append([]*toolListEntry{buildInstructionToolEntry()}, entries...)
}

// toolsetExposesInstructionsTool reports whether a materialized (non-proxy)
// tool in the toolset is named "instructions". Proxy upstreams are not
// live-listed here; a proxy tool with this name is caught by the entries
// check in injectInstructionTool on the list path. On the call path this is
// the only collision guard — a known limitation for proxy upstreams.
func toolsetExposesInstructionsTool(tools []*types.Tool) bool {
	for _, t := range tools {
		if conv.IsProxyTool(t) {
			continue
		}
		baseTool, err := conv.ToBaseTool(t)
		if err != nil {
			continue
		}
		if baseTool.Name == instructionsToolName {
			return true
		}
	}
	return false
}

// instructionSessionGate marks that an MCP session has read the server
// instructions. Stored in Redis under mcpInstructionsRead:{toolset}:{session}
// for 60 minutes; re-Stored on each gated tools/call so active sessions do
// not expire mid-use.
type instructionSessionGate struct {
	ToolsetID string `json:"toolset_id"`
	SessionID string `json:"session_id"`
}

var _ cache.CacheableObject[instructionSessionGate] = (*instructionSessionGate)(nil)

func instructionGateCacheKey(toolsetID, sessionID string) string {
	return "mcpInstructionsRead:" + toolsetID + ":" + sessionID
}

// CacheKey implements cache.CacheableObject.
func (g instructionSessionGate) CacheKey() string {
	return instructionGateCacheKey(g.ToolsetID, g.SessionID)
}

// AdditionalCacheKeys implements cache.CacheableObject.
func (g instructionSessionGate) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (g instructionSessionGate) TTL() time.Duration { return 60 * time.Minute }
