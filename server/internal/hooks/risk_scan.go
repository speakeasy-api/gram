package hooks

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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

	text := extractClaudeText(payload)
	if text == "" {
		return nil
	}

	projectID, ok := s.resolveClaudeScanProjectID(ctx, *payload.SessionID)
	if !ok {
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

	logAttrs := []any{
		attr.SlogEvent("claude_hook_scan_decision"),
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent(payload.HookEventName),
		attr.SlogGenAIConversationID(*payload.SessionID),
		attr.SlogRiskMatched(result != nil),
	}
	if result != nil {
		logAttrs = append(logAttrs,
			attr.SlogRiskPolicyID(result.PolicyID),
			attr.SlogRiskPolicyName(result.PolicyName),
		)
	}
	s.logger.InfoContext(ctx, "claude risk scan completed", logAttrs...)

	return result
}

// resolveClaudeScanProjectID resolves the project_id used to scope a Claude
// hook risk scan. Session metadata cached by the OTEL Logs endpoint wins;
// the plugin-auth context populated by Gram-Key + Gram-Project headers is
// the fallback. Returns ok=false when neither source yields a project_id.
func (s *Service) resolveClaudeScanProjectID(ctx context.Context, sessionID string) (uuid.UUID, bool) {
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	hasCachedMetadata := err == nil
	if hasCachedMetadata {
		pid, perr := uuid.Parse(metadata.ProjectID)
		if perr == nil {
			return pid, true
		}
		s.logger.WarnContext(ctx, "claude risk scan skipped: no project resolved",
			attr.SlogEvent("claude_scan_no_project"),
			attr.SlogHookSource("claude"),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogSessionHasCachedMetadata(true),
			attr.SlogSessionHasAuthCtx(false),
		)
		return uuid.Nil, false
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	hasAuthCtx := ok && authCtx != nil && authCtx.ProjectID != nil
	if !hasAuthCtx {
		s.logger.WarnContext(ctx, "claude risk scan skipped: no project resolved",
			attr.SlogEvent("claude_scan_no_project"),
			attr.SlogHookSource("claude"),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogSessionHasCachedMetadata(false),
			attr.SlogSessionHasAuthCtx(ok && authCtx != nil && authCtx.ProjectID == nil),
		)
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

// scanCodexForEnforcement runs the risk scanner for a Codex hook payload.
// Like Cursor, Codex hooks are authenticated so the project ID is known.
func (s *Service) scanCodexForEnforcement(ctx context.Context, payload *gen.CodexPayload, orgID, projectID string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	text := extractCodexText(payload)
	if text == "" {
		return nil
	}

	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, pid, text)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for Codex hook",
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

// extractCodexText returns the scannable text content from a Codex hook payload.
func extractCodexText(payload *gen.CodexPayload) string {
	switch payload.HookEventName {
	case "UserPromptSubmit":
		if payload.Prompt != nil {
			return *payload.Prompt
		}
	case "PreToolUse", "PermissionRequest":
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
