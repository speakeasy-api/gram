package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// scanClaudeForEnforcement extracts the scannable text from a Claude hook
// payload, resolves the project from session metadata, and runs the risk
// scanner. Returns nil when the scanner is unavailable, the session is not
// yet validated, or no enforcing policy matches.
func (s *Service) scanClaudeForEnforcement(ctx context.Context, payload *gen.ClaudeHookPayload) *risk.ScanResult {
	if s.riskScanner == nil || payload.SessionID == nil {
		return nil
	}

	text := extractClaudeText(payload)
	if text == "" {
		return nil
	}

	metadata, err := s.getSessionMetadata(ctx, *payload.SessionID)
	if err != nil {
		// Session not yet validated; cannot determine project.
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, projectID, text)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Claude hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	if result != nil {
		s.recordClaudeBlockedEvent(ctx, payload, &metadata, result)
	}

	return result
}

// scanCursorForEnforcement runs the risk scanner for a Cursor hook payload.
// Unlike Claude, Cursor hooks are authenticated so the project ID is known.
func (s *Service) scanCursorForEnforcement(ctx context.Context, payload *gen.CursorPayload, orgID, projectID string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	text := extractCursorText(payload)
	if text == "" {
		return nil
	}

	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, pid, text)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Cursor hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	if result != nil {
		s.recordCursorBlockedEvent(ctx, payload, orgID, projectID, result)
	}

	return result
}

// extractClaudeText returns the scannable text content from a Claude hook payload.
func extractClaudeText(payload *gen.ClaudeHookPayload) string {
	switch payload.HookEventName {
	case "UserPromptSubmit":
		if payload.Prompt != nil {
			return *payload.Prompt
		}
	case "PreToolUse":
		if payload.ToolInput != nil {
			b, err := json.Marshal(payload.ToolInput)
			if err != nil {
				return ""
			}
			return string(b)
		}
	}
	return ""
}

// recordClaudeBlockedEvent writes a telemetry log entry with the gram.hook.blocked
// attribute so the trace_summaries MV picks up "blocked" status. The entry shares
// the same trace_id as the original PreToolUse event.
func (s *Service) recordClaudeBlockedEvent(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata, scanResult *risk.ScanResult) {
	if s.telemetryLogger == nil {
		return
	}

	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return
	}

	reason := fmt.Sprintf("Blocked by policy %q: %s", scanResult.PolicyName, scanResult.Description)
	traceID := ""
	if payload.ToolUseID != nil {
		traceID = hashToolCallIDToTraceID(*payload.ToolUseID)
	}
	if traceID == "" {
		traceID = generateTraceID()
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.HookBlockedKey:    reason,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        traceID,
		attr.LogBodyKey:        fmt.Sprintf("Blocked: %s", reason),
		attr.UserEmailKey:      metadata.UserEmail,
		attr.ProjectIDKey:      metadata.ProjectID,
		attr.OrganizationIDKey: metadata.GramOrgID,
		attr.HookSourceKey:     "claude",
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: time.Now(),
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: metadata.GramOrgID,
			ProjectID:      projectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		Attributes: attrs,
	})
}

// recordCursorBlockedEvent writes a telemetry log entry with gram.hook.blocked for Cursor.
func (s *Service) recordCursorBlockedEvent(ctx context.Context, payload *gen.CursorPayload, orgID, projectID string, scanResult *risk.ScanResult) {
	if s.telemetryLogger == nil {
		return
	}

	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		return
	}

	reason := fmt.Sprintf("Blocked by policy %q: %s", scanResult.PolicyName, scanResult.Description)

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.HookBlockedKey:    reason,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Blocked: %s", reason),
		attr.ProjectIDKey:      projectID,
		attr.OrganizationIDKey: orgID,
		attr.HookSourceKey:     "cursor",
	}

	if payload.UserEmail != nil {
		attrs[attr.UserEmailKey] = *payload.UserEmail
	}
	if correlationID := cursorToolCorrelationID(payload); correlationID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(correlationID)
	}
	if payload.ConversationID != nil {
		attrs[attr.GenAIConversationIDKey] = *payload.ConversationID
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: time.Now(),
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: orgID,
			ProjectID:      parsedProjectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		Attributes: attrs,
	})
}

// extractCursorText returns the scannable text content from a Cursor hook payload.
func extractCursorText(payload *gen.CursorPayload) string {
	switch payload.HookEventName {
	case "beforeSubmitPrompt":
		if payload.Prompt != nil {
			return *payload.Prompt
		}
	case "preToolUse", "beforeMCPExecution":
		if payload.ToolInput != nil {
			b, err := json.Marshal(payload.ToolInput)
			if err != nil {
				return ""
			}
			return string(b)
		}
	}
	return ""
}
