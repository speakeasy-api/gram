package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
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
	hookAttrs := s.buildTelemetryAttributesWithMetadata(ctx, payload, metadata)

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID in session metadata", attr.SlogError(err))
		return
	}

	// Build ToolInfo
	toolInfo := telemetry.ToolInfo{
		Name:           hookAttrs.toolName,
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
			Attributes: hookAttrs.toAttrs(),
		})

		s.logger.DebugContext(ctx, "Wrote hook to ClickHouse with metadata",
			attr.SlogEvent("hook_written"),
		)
	}
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
