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
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
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
			Title:           "",
			ReadOnlyHint:    &readOnly,
			DestructiveHint: nil,
			IdempotentHint:  &idempotent,
			OpenWorldHint:   nil,
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

// handleInstructionsToolCall serves a call to the synthetic instructions
// tool: returns the configured instructions (or the not-configured message)
// and marks the session as having read them.
func handleInstructionsToolCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID mcpjsonrpc.ID,
	payload *mcpInputs,
	toolsetID uuid.UUID,
	cfg instructionToolConfig,
	gateCache *cache.TypedCacheObject[instructionSessionGate],
) (json.RawMessage, error) {
	markInstructionsRead(ctx, logger, gateCache, toolsetID, payload)

	text := cfg.Instructions
	if text == "" {
		text = instructionsNotConfiguredMessage
	}
	return buildInstructionsTextResult(ctx, logger, reqID, text)
}

// buildInstructionGateResponse answers a gated tools/call: the blocked tool
// is NOT executed; the agent receives the instructions plus a retry note as
// a successful result (agents handle content responses more predictably
// than JSON-RPC errors, and the round trip cost stays at exactly one).
func buildInstructionGateResponse(
	ctx context.Context,
	logger *slog.Logger,
	reqID mcpjsonrpc.ID,
	instructions string,
	blockedTool string,
) (json.RawMessage, error) {
	text := instructions + "\n\n---\nThis MCP server requires reading the server instructions (above) before other tools run. Your call to \"" + blockedTool + "\" was not executed. Please retry your original call now."
	return buildInstructionsTextResult(ctx, logger, reqID, text)
}

func buildInstructionsTextResult(ctx context.Context, logger *slog.Logger, reqID mcpjsonrpc.ID, text string) (json.RawMessage, error) {
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     text,
		MimeType: nil,
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize instructions chunk").LogError(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: nil,
			IsError:           false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize instructions response").LogError(ctx, logger)
	}
	return response, nil
}

// markInstructionsRead stores (or refreshes) the session gate flag. Redis
// failures are logged and ignored — the gate fails open, never breaking a
// tool call.
func markInstructionsRead(ctx context.Context, logger *slog.Logger, gateCache *cache.TypedCacheObject[instructionSessionGate], toolsetID uuid.UUID, payload *mcpInputs) {
	if !payload.sessionProvided {
		return
	}
	if err := gateCache.Store(ctx, instructionSessionGate{ToolsetID: toolsetID.String(), SessionID: payload.sessionID}); err != nil {
		logger.WarnContext(ctx, "failed to store instruction gate flag", attr.SlogError(err))
	}
}

// captureInstructionGateEvent emits the product metric for a gate trigger —
// the measure of how often agents would have skipped reading instructions.
func captureInstructionGateEvent(ctx context.Context, logger *slog.Logger, productMetrics *posthog.Posthog, payload *mcpInputs, toolsetSlug, toolsetID, blockedTool string) {
	if err := productMetrics.CaptureEvent(ctx, "mcp_instructions_gate_triggered", payload.sessionID, map[string]any{
		"project_id":           payload.projectID.String(),
		"toolset_slug":         toolsetSlug,
		"toolset_id":           toolsetID,
		"blocked_tool":         blockedTool,
		"mcp_session_id":       payload.sessionID,
		"disable_notification": true,
	}); err != nil {
		logger.ErrorContext(ctx, "failed to capture mcp_instructions_gate_triggered event", attr.SlogError(err))
	}
}
