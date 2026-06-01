package hooks

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/riskscope"
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

	projectID, ok := s.resolveClaudeScanProjectID(ctx, *payload.SessionID)
	if !ok {
		return nil
	}

	inputScope, ok := hookEventToInputScope(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, projectID, text, inputScope)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Claude hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

// resolveClaudeScanProjectID resolves the project_id used to scope a Claude
// hook risk scan. Session metadata cached by the OTEL Logs endpoint wins;
// the plugin-auth context populated by Gram-Key + Gram-Project headers is
// the fallback. Returns ok=false when neither source yields a project_id.
func (s *Service) resolveClaudeScanProjectID(ctx context.Context, sessionID string) (uuid.UUID, bool) {
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		pid, perr := uuid.Parse(metadata.ProjectID)
		if perr == nil {
			return pid, true
		}
		return uuid.Nil, false
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return uuid.Nil, false
	}
	return *authCtx.ProjectID, true
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

	inputScope, ok := hookEventToInputScope(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, pid, text, inputScope)
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

	inputScope, ok := hookEventToInputScope(hookEvent)
	if !ok {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, pid, text, inputScope)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Codex hook",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
		)
		return nil
	}

	return result
}

func hookEventToInputScope(hookEvent HookEvent) (string, bool) {
	switch hookEvent {
	case HookEventUserPromptSubmit, HookEventBeforeSubmitPrompt:
		return riskscope.InputScopeUserMessage, true
	case HookEventPreToolUse, HookEventBeforeMCPExecution, HookEventPermissionRequest:
		return riskscope.InputScopeToolRequest, true
	case HookEventPostToolUse:
		return riskscope.InputScopeToolResponse, true
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
