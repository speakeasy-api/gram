package hooks

import (
	"context"
	"fmt"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// hookLogAttrs contains standardized hook attributes across all hook types
type hookLogAttrs struct {
	eventSource    string
	toolName       string
	hookEvent      string
	spanID         string
	traceID        string
	logBody        string
	userEmail      string
	projectID      string
	organizationID string
	hookSource     string
	conversationID string
	toolCallID     string
	toolCallSource string
	error          any
	isInterrupt    *bool
	toolInput      any
	toolResponse   any
}

// toAttrs converts hookResultAttrs to the ClickHouse-compatible map
func (h *hookLogAttrs) toAttrs() map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.EventSourceKey:    h.eventSource,
		attr.ToolNameKey:       h.toolName,
		attr.HookEventKey:      h.hookEvent,
		attr.SpanIDKey:         h.spanID,
		attr.TraceIDKey:        h.traceID,
		attr.LogBodyKey:        h.logBody,
		attr.UserEmailKey:      h.userEmail,
		attr.ProjectIDKey:      h.projectID,
		attr.OrganizationIDKey: h.organizationID,
		attr.HookSourceKey:     h.hookSource,
	}

	if h.conversationID != "" {
		attrs[attr.GenAIConversationIDKey] = h.conversationID
	}
	if h.toolCallID != "" {
		attrs[attr.GenAIToolCallIDKey] = h.toolCallID
	}
	if h.toolCallSource != "" {
		attrs[attr.ToolCallSourceKey] = h.toolCallSource
	}
	if h.error != nil {
		attrs[attr.HookErrorKey] = h.error
	}
	if h.isInterrupt != nil {
		attrs[attr.HookIsInterruptKey] = *h.isInterrupt
	}
	if h.toolInput != nil {
		attrs[attr.GenAIToolCallArgumentsKey] = h.toolInput
	}
	if h.toolResponse != nil {
		attrs[attr.GenAIToolCallResultKey] = h.toolResponse
	}

	return attrs
}

// buildCursorHookAttributes creates attributes for a Cursor hook event with auth context
func (s *Service) buildCursorHookAttributes(ctx context.Context, payload *gen.CursorHookPayload, projectID, orgID string) hookLogAttrs {
	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	traceID := generateTraceID()
	// Hash toolUseID to create trace ID if available
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		traceID = hashToolCallIDToTraceID(*payload.ToolUseID)
	}

	conversationID := ""
	if payload.ConversationID != nil {
		conversationID = *payload.ConversationID
	}

	toolCallID := ""
	if payload.ToolUseID != nil {
		toolCallID = *payload.ToolUseID
	}

	userEmail := ""
	if payload.UserEmail != nil {
		userEmail = *payload.UserEmail
	}

	// Parse MCP tool names
	toolCallSource := ""
	parsedToolName := toolName
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			toolCallSource = parts[1]
			parsedToolName = parts[2]
		}
	}

	result := hookLogAttrs{
		eventSource:    string(telemetry.EventSourceHook),
		toolName:       parsedToolName,
		hookEvent:      payload.HookEventName,
		spanID:         generateSpanID(),
		traceID:        traceID,
		logBody:        fmt.Sprintf("Tool: %s, Hook: %s", parsedToolName, payload.HookEventName),
		userEmail:      userEmail,
		projectID:      projectID,
		organizationID: orgID,
		hookSource:     "cursor",
		conversationID: conversationID,
		toolCallID:     toolCallID,
		toolCallSource: toolCallSource,
		error:          payload.Error,
		isInterrupt:    nil, // Cursor doesn't have isInterrupt
		toolInput:      payload.ToolInput,
		toolResponse:   payload.ToolResponse,
	}

	return result
}

// buildTelemetryAttributesWithMetadata creates attributes for a hook event with session metadata
func (s *Service) buildTelemetryAttributesWithMetadata(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) hookLogAttrs {
	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	hookSource := "claude"
	if metadata.ServiceName != "" {
		hookSource = metadata.ServiceName
	}

	traceID := generateTraceID()
	// Hash toolUseID to create trace ID if available
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		traceID = hashToolCallIDToTraceID(*payload.ToolUseID)
	}

	conversationID := ""
	if payload.SessionID != nil {
		conversationID = *payload.SessionID
	}

	toolCallID := ""
	if payload.ToolUseID != nil {
		toolCallID = *payload.ToolUseID
	}

	// Parse MCP tool names
	toolCallSource := ""
	parsedToolName := toolName
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			toolCallSource = parts[1]
			parsedToolName = parts[2]
		}
	}

	result := hookLogAttrs{
		eventSource:    string(telemetry.EventSourceHook),
		toolName:       parsedToolName,
		hookEvent:      payload.HookEventName,
		spanID:         generateSpanID(),
		traceID:        traceID,
		logBody:        fmt.Sprintf("Tool: %s, Hook: %s", parsedToolName, payload.HookEventName),
		userEmail:      metadata.UserEmail,
		projectID:      metadata.ProjectID,
		organizationID: metadata.GramOrgID,
		hookSource:     hookSource,
		conversationID: conversationID,
		toolCallID:     toolCallID,
		toolCallSource: toolCallSource,
		error:          payload.Error,
		isInterrupt:    payload.IsInterrupt,
		toolInput:      payload.ToolInput,
		toolResponse:   payload.ToolResponse,
	}

	return result
}
