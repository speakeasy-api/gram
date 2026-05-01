package hooks

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// scanClaudeForEnforcement extracts the scannable text from a Claude hook
// payload, resolves the project from session metadata, and runs the risk
// scanner. Returns nil when the scanner is unavailable, the session is not
// yet validated, or no enforcing policy matches.
func (s *Service) scanClaudeForEnforcement(ctx context.Context, payload *gen.ClaudePayload) *risk.ScanResult {
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

	return result
}

// renderUserBlockReason returns the message shown to the agent when a tool
// call or prompt is denied. When the policy carries a non-empty user_message,
// that overrides the default Speakeasy-branded format; otherwise the supplied
// audit reason is rendered verbatim. The audit reason itself is what gets
// stored in ClickHouse traces — the user_message override only affects what
// the agent / end user sees.
func renderUserBlockReason(userMessage *string, auditReason string) string {
	if userMessage != nil && *userMessage != "" {
		return *userMessage
	}
	return auditReason
}

// extractClaudeText returns the scannable text content from a Claude hook payload.
func extractClaudeText(payload *gen.ClaudePayload) string {
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
	case "PostToolUse":
		if payload.ToolResponse != nil {
			b, err := json.Marshal(payload.ToolResponse)
			if err != nil {
				return ""
			}
			return string(b)
		}
	}
	return ""
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
