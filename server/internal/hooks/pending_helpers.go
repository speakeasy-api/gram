package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/telemetry"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// bufferHook stores a hook payload in Redis for later processing using atomic RPUSH
func (s *Service) bufferHook(ctx context.Context, sessionID string, payload *gen.ClaudeHookPayload) error {
	// Use atomic RPUSH operation to append to the list
	// This eliminates the race condition from read-modify-write
	ttl := 5 * time.Minute // TTL for buffered hooks. This is very generous. Could be lower since this can trigger through an unauthenticated endpoint.
	if err := s.cache.ListAppend(ctx, hookPendingCacheKey(sessionID), payload, ttl); err != nil {
		return fmt.Errorf("append hook to list: %w", err)
	}

	s.logger.DebugContext(ctx, "Buffered hook in Redis",
		attr.SlogEvent("hook_buffered"),
	)

	return nil
}

// writeHookToClickHouseWithMetadata writes a hook event to ClickHouse with full session context
func (s *Service) writeHookToClickHouseWithMetadata(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) {
	attrs := s.buildTelemetryAttributesWithMetadata(ctx, payload, metadata)
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

	if s.telemetryService != nil {
		s.telemetryService.CreateLog(telemetry.LogParams{
			Timestamp:  time.Now(),
			ToolInfo:   toolInfo,
			Attributes: attrs,
		})

		s.logger.DebugContext(ctx, "Wrote hook to ClickHouse with metadata",
			attr.SlogEvent("hook_written"),
		)
	}
}

// buildTelemetryAttributesWithMetadata creates attributes for a hook event with session metadata
func (s *Service) buildTelemetryAttributesWithMetadata(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) map[attr.Key]any {
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

	if payload.ToolError != nil {
		attrs[attr.HookErrorKey] = payload.ToolError
	}

	isMCP := false
	// Parse MCP tool names
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.ToolCallSourceKey] = parts[1]
			attrs[attr.ToolNameKey] = parts[2]
			isMCP = true
		}
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

	// Only map MCP servers to human-readable names
	if isMCP {
		// Check cache for existing mapping
		source, ok := attrs[attr.ToolCallSourceKey].(string)
		if ok && source != "" {
			key := nameMappingCacheKey(source)
			var cachedName string
			err := s.cache.Get(ctx, key, &cachedName)
			if err == nil && cachedName != "" {
				// Use cached mapping
				attrs[attr.ToolCallSourceKey] = cachedName
			} else if s.temporalEnv != nil {
				// No cached mapping - trigger async workflow to generate one
				// Fire-and-forget - don't block hook processing
				go func() {
					bgCtx := context.Background()
					_, err := background.ExecuteProcessNameMappingWorkflow(bgCtx, s.temporalEnv, background.ProcessNameMappingWorkflowParams{
						ServerName:    source,
						ToolCallAttrs: convertAttrsToMap(attrs),
						OrgID:         metadata.GramOrgID,
						ProjectID:     metadata.ProjectID,
					})
					if err != nil {
						s.logger.ErrorContext(bgCtx, "failed to start name mapping workflow",
							slog.String("server_name", source),
							attr.SlogError(err),
						)
					}
				}()
			}
		}
	}

	return attrs
}

// nameMappingCacheKey generates the Redis key for a server name mapping
func nameMappingCacheKey(serverName string) string {
	return fmt.Sprintf("hooks:name_mapping:%s", serverName)
}

// convertAttrsToMap converts attr.Key map to string map for workflow params
func convertAttrsToMap(attrs map[attr.Key]any) map[string]any {
	result := make(map[string]any, len(attrs))
	for k, v := range attrs {
		result[string(k)] = v
	}
	return result
}

// flushPendingHooks retrieves all buffered hooks for a session and writes them to ClickHouse
func (s *Service) flushPendingHooks(ctx context.Context, sessionID string, metadata *SessionMetadata) {
	// Use LRANGE to get all payloads from the list atomically
	var payloads []gen.ClaudeHookPayload
	key := hookPendingCacheKey(sessionID)

	if err := s.cache.ListRange(ctx, key, 0, -1, &payloads); err != nil {
		s.logger.DebugContext(ctx, "No pending hooks to flush or error reading list", attr.SlogError(err))
		return
	}

	if len(payloads) == 0 {
		return
	}

	// Write all payloads to ClickHouse
	for i := range payloads {
		s.writeHookToClickHouseWithMetadata(ctx, &payloads[i], metadata)
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("Flushed %d pending hooks to ClickHouse", len(payloads)))

	// Delete the list after successful processing
	if err := s.cache.Delete(ctx, key); err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete hook buffer", attr.SlogError(err))
	}
}
