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
// payload, resolves the project (session metadata wins; falls back to the
// plugin-auth context populated by Gram-Key + Gram-Project headers), and
// runs the risk scanner. Returns nil when the scanner is unavailable, the
// project cannot be resolved, or no enforcing policy matches.
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
	if text == "" {
		return nil
	}

	orgID, projectID, userID, ok := s.resolveClaudeScanContext(ctx, *payload.SessionID)
	if !ok {
		return nil
	}

	messageType, ok := hookEventToMessageType(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForUserEnforcement(ctx, orgID, projectID, userID, text, messageType)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Claude hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

// resolveClaudeScanContext resolves the org/project/user used to scope a Claude
// hook risk scan. Session metadata cached by the OTEL Logs endpoint wins; the
// plugin-auth context populated by Gram-Key + Gram-Project headers is the
// fallback. Returns ok=false when neither source yields a project_id.
func (s *Service) resolveClaudeScanContext(ctx context.Context, sessionID string) (string, uuid.UUID, string, bool) {
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		pid, perr := uuid.Parse(metadata.ProjectID)
		if perr == nil {
			userID := metadata.UserID
			if userID == "" {
				userID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)
			}
			return metadata.GramOrgID, pid, userID, true
		}
		return "", uuid.Nil, "", false
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return "", uuid.Nil, "", false
	}
	return authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, true
}

// scanCursorForEnforcement runs the risk scanner for a Cursor hook payload.
// Unlike Claude, Cursor hooks are authenticated so the project ID is known.
func (s *Service) scanCursorForEnforcement(ctx context.Context, payload *gen.CursorPayload, orgID, projectID string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	hookEvent, ok := parseCursorHookEvent(payload.HookEventName)
	if !ok {
		return nil
	}

	text := extractCursorText(payload, hookEvent)
	if text == "" {
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

	result, err := s.riskScanner.ScanForUserEnforcement(ctx, orgID, pid, s.resolveCursorUserID(ctx, payload, orgID), text, messageType)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Cursor hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

func (s *Service) resolveCursorUserID(ctx context.Context, payload *gen.CursorPayload, orgID string) string {
	return s.resolveUserByEmail(ctx, conv.PtrValOr(payload.UserEmail, ""), orgID)
}

// scanCodexForEnforcement runs the risk scanner for a Codex hook payload.
// Like Cursor, Codex hooks are authenticated so the project ID is known.
func (s *Service) scanCodexForEnforcement(ctx context.Context, payload *gen.CodexPayload, orgID, projectID string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	hookEvent, ok := parseCodexHookEvent(payload.HookEventName)
	if !ok {
		return nil
	}

	text := extractCodexText(payload, hookEvent)
	if text == "" {
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

	result, err := s.riskScanner.ScanForUserEnforcement(ctx, orgID, pid, s.resolveCodexUserID(ctx, payload, orgID, projectID), text, messageType)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Codex hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

func (s *Service) resolveCodexUserID(ctx context.Context, payload *gen.CodexPayload, orgID, projectID string) string {
	return s.codexSessionMetadata(ctx, payload, orgID, projectID).UserID
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
