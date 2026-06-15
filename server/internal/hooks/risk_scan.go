package hooks

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// scanClaudeForEnforcement extracts the scannable text from a Claude hook
// payload, resolves the project (plugin-auth context populated by Gram-Key +
// Gram-Project wins; session metadata is the legacy fallback), and runs the
// risk scanner. Returns nil when the scanner is unavailable, the project cannot
// be resolved, or no enforcing policy matches.
//
// The authCtx fallback is critical for UserPromptSubmit on the very first
// hook of a session: Claude Code's OTEL Logs exporter is async, so the
// `/rpc/hooks.otel/v1/logs` request that seeds session metadata in Redis
// can land after the first UserPromptSubmit. Without this fallback, the
// first prompt of every session slips through unscanned even when the
// plugin authenticated the request.
func (s *Service) scanClaudeForEnforcement(ctx context.Context, payload *gen.ClaudePayload) *risk.ScanResult {
	if s.riskScanner == nil || payload.SessionID == nil {
		return nil
	}

	hookEvent, ok := parseClaudeHookEvent(payload.HookEventName)
	if !ok {
		return nil
	}

	text := extractClaudeText(payload, hookEvent)
	// A no-arg/no-output tool call carries an empty body but still names a tool
	// (+ MCP server/function) a tool-scoped prompt policy can match, so only skip
	// when there is neither body nor tool attribution.
	toolName := conv.PtrValOr(payload.ToolName, "")
	if text == "" && toolName == "" {
		return nil
	}

	scanContext, ok := s.resolveClaudeScanContext(ctx, *payload.SessionID)
	if !ok {
		return nil
	}

	messageType, ok := hookEventToMessageType(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, scanContext.organizationID, scanContext.projectID, scanContext.userID, text, messageType, toolName)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Claude hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

type claudeScanContext struct {
	organizationID string
	projectID      uuid.UUID
	userID         string
}

// resolveClaudeScanContext resolves the org/project/user used to scope a Claude
// hook risk scan. The plugin-auth context populated by Gram-Key + Gram-Project
// wins when present. Session metadata cached by the OTEL Logs endpoint remains
// the fallback for legacy hooks without plugin auth.
func (s *Service) resolveClaudeScanContext(ctx context.Context, sessionID string) (claudeScanContext, bool) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if ok && authCtx != nil && authCtx.ProjectID != nil {
		return claudeScanContext{
			organizationID: authCtx.ActiveOrganizationID,
			projectID:      *authCtx.ProjectID,
			userID:         authCtx.UserID,
		}, true
	}

	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err != nil {
		return claudeScanContext{organizationID: "", projectID: uuid.Nil, userID: ""}, false
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return claudeScanContext{organizationID: "", projectID: uuid.Nil, userID: ""}, false
	}

	userID := metadata.UserID
	if userID == "" {
		userID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)
	}

	return claudeScanContext{
		organizationID: metadata.GramOrgID,
		projectID:      projectID,
		userID:         userID,
	}, true
}

// scanCursorForEnforcement runs the risk scanner for a Cursor hook payload.
// Unlike Claude, Cursor hooks are authenticated so the project ID is known.
func (s *Service) scanCursorForEnforcement(ctx context.Context, payload *gen.CursorPayload, orgID, projectID, userID string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	hookEvent, ok := parseCursorHookEvent(payload.HookEventName)
	if !ok {
		return nil
	}

	text := extractCursorText(payload, hookEvent)
	// Empty body + tool attribution still matters for tool-scoped policies; only
	// skip when there is neither.
	toolName := conv.PtrValOr(payload.ToolName, "")
	if text == "" && toolName == "" {
		return nil
	}

	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}

	messageType, ok := hookEventToMessageType(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, orgID, pid, userID, text, messageType, toolName)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Cursor hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

// scanCodexForEnforcement runs the risk scanner for a Codex hook payload.
// Like Cursor, Codex hooks are authenticated so the project ID is known.
func (s *Service) scanCodexForEnforcement(ctx context.Context, payload *gen.CodexPayload, orgID, projectID, userID string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	hookEvent, ok := parseCodexHookEvent(payload.HookEventName)
	if !ok {
		return nil
	}

	text := extractCodexText(payload, hookEvent)
	// Empty body + tool attribution still matters for tool-scoped policies; only
	// skip when there is neither.
	toolName := conv.PtrValOr(payload.ToolName, "")
	if text == "" && toolName == "" {
		return nil
	}

	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}

	messageType, ok := hookEventToMessageType(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, orgID, pid, userID, text, messageType, toolName)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Codex hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

func hookEventToMessageType(hookEvent HookEvent) (message.Type, bool) {
	switch hookEvent {
	case HookEventUserPromptSubmit, HookEventBeforeSubmitPrompt:
		return message.User, true
	case HookEventPreToolUse, HookEventBeforeMCPExecution, HookEventPermissionRequest:
		return message.ToolRequest, true
	case HookEventPostToolUse:
		return message.ToolResponse, true
	default:
		return "", false
	}
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
func extractClaudeText(payload *gen.ClaudePayload, hookEvent HookEvent) string {
	switch hookEvent {
	case HookEventUserPromptSubmit:
		if payload.Prompt != nil {
			return *payload.Prompt
		}
	case HookEventPreToolUse:
		if payload.ToolInput != nil {
			b, err := json.Marshal(payload.ToolInput)
			if err != nil {
				return ""
			}
			return string(b)
		}
	case HookEventPostToolUse:
		if payload.ToolResponse != nil {
			b, err := json.Marshal(payload.ToolResponse)
			if err != nil {
				return ""
			}
			return string(b)
		}
	default:
		return ""
	}
	return ""
}

// extractCursorText returns the scannable text content from a Cursor hook payload.
func extractCursorText(payload *gen.CursorPayload, hookEvent HookEvent) string {
	switch hookEvent {
	case HookEventBeforeSubmitPrompt:
		if payload.Prompt != nil {
			return *payload.Prompt
		}
	case HookEventPreToolUse, HookEventBeforeMCPExecution:
		if payload.ToolInput != nil {
			b, err := json.Marshal(payload.ToolInput)
			if err != nil {
				return ""
			}
			return string(b)
		}
	default:
		return ""
	}
	return ""
}

// extractCodexText returns the scannable text content from a Codex hook payload.
func extractCodexText(payload *gen.CodexPayload, hookEvent HookEvent) string {
	switch hookEvent {
	case HookEventUserPromptSubmit:
		if payload.Prompt != nil {
			return *payload.Prompt
		}
	case HookEventPreToolUse, HookEventPermissionRequest:
		if payload.ToolInput != nil {
			b, err := json.Marshal(payload.ToolInput)
			if err != nil {
				return ""
			}
			return string(b)
		}
	default:
		return ""
	}
	return ""
}
