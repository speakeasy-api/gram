package litellm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// mcpProvenanceURN is the gram_urn stamped on a LiteLLM-observed MCP tool-call
// row. It must not start with "urn:uuid:", the prefix trace_summaries_mv drops,
// so the row reaches the usage and attribution summaries.
const mcpProvenanceURN = "litellm:guardrail:tool_call"

// mcpProvenanceHookSource is the gram.hook.source value, materialized into the
// telemetry_logs.hook_source column, that labels a row as observed via the
// customer's LiteLLM proxy.
const mcpProvenanceHookSource = "litellm"

// mcpToolCallProvenance is one LiteLLM-observed MCP tool call to record.
type mcpToolCallProvenance struct {
	// ToolCallID is the LiteLLM/OpenAI tool_call id. Its hash is the row's
	// trace_id, so every channel that observes this call groups into one trace.
	ToolCallID string

	// ToolName is the full namespaced tool name (mcp__<server>__<tool>).
	ToolName string

	// Server is the <server> segment, lower-cased.
	Server string

	// Identity is the synthetic shadowmcp.ToolNamespaceURL identity for Server.
	Identity string
}

// mcpProvenanceInput carries the attribution and identity for a batch of
// LiteLLM-observed MCP tool calls recorded from one guardrail response.
type mcpProvenanceInput struct {
	// ProjectID is the Gram project the calls belong to.
	ProjectID string

	// OrganizationID is the Gram organization the calls belong to.
	OrganizationID string

	// UserID is the resolved Gram user id, when the hooks path found one.
	UserID string

	// UserEmail is the acting user's email; the summaries attribute usage to it.
	UserEmail string

	// SessionID is the agent session id, mapped to the chat id on the row.
	SessionID string

	// CallID is the LiteLLM call id that produced the response.
	CallID string

	// TraceID is the LiteLLM trace id, when the request carried one.
	TraceID string

	// Model is the responding model, when reported.
	Model string

	// ToolCalls are the MCP tool calls to record, one row each.
	ToolCalls []mcpToolCallProvenance
}

// mcpToolCallProvenanceFrom extracts the MCP tool calls from a LiteLLM
// guardrail response's tool_calls array. Each entry is an OpenAI tool call
// object (`{"id":...,"function":{"name":...}}`); the chunk form where
// function.arguments is absent is accepted too. Entries without an id or
// without a namespaced mcp__ name (bare functions, the Cursor MCP:<fn> form)
// are skipped, so only calls with a derivable server identity are recorded.
func mcpToolCallProvenanceFrom(toolCalls []any) []mcpToolCallProvenance {
	out := make([]mcpToolCallProvenance, 0, len(toolCalls))
	for _, raw := range toolCalls {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(anyString(call["id"]))
		if id == "" {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		name := strings.TrimSpace(anyString(fn["name"]))
		if !strings.HasPrefix(name, "mcp__") || !toolref.IsMCPToolName(name) {
			continue
		}
		server := strings.ToLower(toolref.MCPServerOf(name))
		identity := shadowmcp.ToolNamespaceURL(server)
		if identity == "" {
			continue
		}
		out = append(out, mcpToolCallProvenance{
			ToolCallID: id,
			ToolName:   name,
			Server:     server,
			Identity:   identity,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// recordMCPToolCallProvenance writes one telemetry_logs row per
// LiteLLM-observed MCP tool call so trace_summaries yields usage, last-called,
// and per-user attribution for each synthetic server identity. It is
// best-effort: it logs a warning on failure and never returns an error to the
// request path.
func (s *Service) recordMCPToolCallProvenance(ctx context.Context, in mcpProvenanceInput) {
	if s.telemetryLogger == nil || in.ProjectID == "" || len(in.ToolCalls) == 0 {
		return
	}

	timestamp := time.Now().UTC()
	chatID := ""
	if in.SessionID != "" {
		chatID = chat.SessionIDToChatID(in.SessionID).String()
	}

	params := make([]telemetry.LogParams, 0, len(in.ToolCalls))
	for _, call := range in.ToolCalls {
		attrs := map[attr.Key]any{
			attr.EventSourceKey:     string(telemetry.EventSourceHook),
			attr.HookSourceKey:      mcpProvenanceHookSource,
			attr.HookEventKey:       string(hooks.HookEventPostToolUse),
			attr.TraceIDKey:         hooks.HashToolCallIDToTraceID(call.ToolCallID),
			attr.SpanIDKey:          generateProvenanceSpanID(),
			attr.MCPServerURLKey:    call.Identity,
			attr.MCPMatchKey:        call.Identity,
			attr.ToolCallSourceKey:  call.Server,
			attr.GenAIToolNameKey:   call.ToolName,
			attr.GenAIToolCallIDKey: call.ToolCallID,
			attr.LogBodyKey:         "MCP tool call: " + call.ToolName,
		}
		if chatID != "" {
			attrs[attr.GenAIConversationIDKey] = chatID
		}
		if in.Model != "" {
			attrs[attr.GenAIResponseModelKey] = in.Model
		}
		if in.UserEmail != "" {
			attrs[attr.LiteLLMUserEmailKey] = in.UserEmail
		}
		if in.CallID != "" {
			attrs[attr.LiteLLMCallIDKey] = in.CallID
		}
		if in.TraceID != "" {
			attrs[attr.LiteLLMTraceIDKey] = in.TraceID
		}
		if in.OrganizationID != "" {
			attrs[attr.LiteLLMOrganizationIDKey] = in.OrganizationID
		}

		params = append(params, telemetry.LogParams{
			Timestamp: timestamp,
			ToolInfo: telemetry.ToolInfo{
				ID:             "",
				URN:            mcpProvenanceURN,
				Name:           call.ToolName,
				ProjectID:      in.ProjectID,
				DeploymentID:   "",
				FunctionID:     nil,
				OrganizationID: in.OrganizationID,
			},
			UserInfo:   telemetry.UserInfoByIDAndEmail(in.UserID, in.UserEmail),
			Attributes: attrs,
		})
	}

	if err := s.telemetryLogger.LogBulk(ctx, params); err != nil {
		s.logger.WarnContext(ctx, "failed to record LiteLLM MCP tool-call provenance",
			attr.SlogError(err),
			attr.SlogProjectID(in.ProjectID),
			attr.SlogLiteLLMCallID(in.CallID),
		)
	}
}

// generateProvenanceSpanID returns a W3C-compliant 16-hex-character span id so
// each provenance row carries a distinct span within its trace.
func generateProvenanceSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
