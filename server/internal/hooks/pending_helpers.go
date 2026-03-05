package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)


// bufferHook stores a hook payload in Redis for later processing
func (s *Service) bufferHook(ctx context.Context, sessionID string, payload *gen.ClaudePayload) error {
	currentCache, err := s.hookBufferCache.Get(ctx, hookPendingCacheKey(sessionID))
	if err == nil && currentCache.Payloads == nil {
		currentCache = ClaudePayloadCache{
			SessionID: sessionID,
			Payloads:  make([]gen.ClaudePayload, 0),
		}
	}

	currentCache.Payloads = append(currentCache.Payloads, *payload)
	if err := s.hookBufferCache.Store(ctx, currentCache); err != nil {
		return fmt.Errorf("store hook in cache: %w", err)
	}

	s.logger.DebugContext(ctx, "Buffered hook in Redis",
		attr.SlogEvent("hook_buffered"),
	)

	return nil
}

// writeHookToClickHouseWithMetadata writes a hook event to ClickHouse with full session context
func (s *Service) writeHookToClickHouseWithMetadata(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) {
	attrs := s.buildTelemetryAttributesWithMetadata(payload, metadata)
	toolName, ok := attrs[attr.ToolNameKey].(string) //  Make sure this comes from here so that we get the parsed tool name
	if !ok {
		s.logger.ErrorContext(ctx, "Tool name not found in attributes")
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID in session metadata", attr.SlogError(err))
		return
	}

	// Build ToolInfo
	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: metadata.GramOrgID,
		ProjectID:      projectID.String(),
		ID:             "",
		URN:            "",
		DeploymentID:   "",
		FunctionID:     nil,
	}

	s.telemetryService.CreateLog(telemetry.LogParams{
		Timestamp:  time.Now(),
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	s.logger.DebugContext(ctx, "Wrote hook to ClickHouse with metadata",
		attr.SlogEvent("hook_written"),
	)
}

// buildTelemetryAttributesWithMetadata creates attributes for a hook event with session metadata
func (s *Service) buildTelemetryAttributesWithMetadata(payload *gen.ClaudePayload, metadata *SessionMetadata) map[attr.Key]any {
	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	hookSource := "claude"
	if metadata.ServiceName != "" {
		hookSource = metadata.ServiceName
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Tool: %s, Hook: %s", toolName, payload.HookEventName),
		attr.UserEmailKey:      metadata.UserEmail,
		attr.ProjectIDKey:      metadata.ProjectID,
		attr.OrganizationIDKey: metadata.GramOrgID,
		attr.HookSourceKey:     hookSource,
	}

	// Parse MCP tool names
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		attrs[attr.ToolCallSourceKey] = parts[1]
		attrs[attr.ToolNameKey] = parts[2]
	}

	// Hash toolUseID to create trace ID if available
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(*payload.ToolUseID)
	}
	if payload.SessionID != nil {
		attrs[attr.GenAIConversationIDKey] = *payload.SessionID
	}
	if payload.ToolUseID != nil {
		attrs[attr.GenAIToolCallIDKey] = *payload.ToolUseID
	}
	if payload.ToolInput != nil {
		attrs[attr.GenAIToolCallArgumentsKey] = payload.ToolInput
	}
	if payload.ToolResponse != nil {
		attrs[attr.GenAIToolCallResultKey] = payload.ToolResponse
	}

	return attrs
}

// flushPendingHooks retrieves all buffered hooks for a session and writes them to ClickHouse
func (s *Service) flushPendingHooks(ctx context.Context, sessionID string, metadata *SessionMetadata) {
	pending, err := s.hookBufferCache.Get(ctx, hookPendingCacheKey(sessionID))
	if err != nil || pending.Payloads == nil {
		return
	}
	for _, payload := range pending.Payloads {
		s.writeHookToClickHouseWithMetadata(ctx, &payload, metadata)
	}
	s.logger.InfoContext(ctx, fmt.Sprintf("Flushed %d pending hooks to ClickHouse", len(pending.Payloads)))
	if err := s.hookBufferCache.DeleteByKey(ctx, hookPendingCacheKey(sessionID)); err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete hook buffer", attr.SlogError(err))
	}
}
