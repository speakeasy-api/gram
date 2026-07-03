package hooks

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

func (s *Service) scanUserPromptForEnforcement(ctx context.Context, ev *hookevents.UserPromptSubmit) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, ev.Prompt, message.User, "")
}

func (s *Service) scanToolRequestForEnforcement(ctx context.Context, ev *hookevents.BeforeToolUse) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, marshalToJSON(ev.ToolInput), message.ToolRequest, ev.ToolName)
}

func (s *Service) scanMCPRequestForEnforcement(ctx context.Context, ev *hookevents.BeforeMCPExecution) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, marshalToJSON(ev.ToolInput), message.ToolRequest, ev.ToolName)
}

func (s *Service) scanPermissionRequestForEnforcement(ctx context.Context, ev *hookevents.PermissionRequest) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, marshalToJSON(ev.ToolInput), message.ToolRequest, ev.ToolName)
}

func (s *Service) scanHookEventForEnforcement(ctx context.Context, ev hookevents.Event, text string, messageType message.Type, toolName string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	// Empty body + tool attribution still matters for tool-scoped policies; only
	// skip when there is neither.
	if text == "" && toolName == "" {
		return nil
	}

	if messageType == "" {
		return nil
	}

	if ev.Context.OrganizationID == "" || ev.Context.ProjectID == uuid.Nil {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, ev.Context.OrganizationID, ev.Context.ProjectID, ev.Context.User.ID, text, messageType, toolName)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for hook event",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
			attr.SlogHookSource(string(ev.Provider)),
			attr.SlogHookEvent(ev.RawEventType),
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

// warnMatchMaxLen bounds the matched value shown in a challenge warning. The
// match is sensitive; it appears ONLY in this ephemeral agent-facing message,
// never in logs, ClickHouse, tool_call_blocks, or audit.
const warnMatchMaxLen = 120

// warnAcknowledged reports whether the user has a live acknowledgement for a
// warn (challenge) match, so the retried call should be allowed. Only meaningful
// when scanResult.Action == "warn".
func (s *Service) warnAcknowledged(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult, toolName string) bool {
	if s.riskScanner == nil || scanResult == nil {
		return false
	}
	return s.riskScanner.HasAcknowledgedChallenge(ctx, ev.Context.ProjectID, ev.Context.User.ID, scanResult.PolicyID, toolName)
}

// warnDenyReason records the challenge and returns the user-facing warning + an
// acknowledgement link. ok=false means an ack link could not be produced
// (missing site URL / cache / user id) — the caller MUST fall back to a plain
// block (fail-safe): a warn must never silently allow.
func (s *Service) warnDenyReason(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult, toolName string) (reason string, ok bool) {
	if s.siteURL == nil || s.cache == nil || ev.Context.User.ID == "" {
		return "", false
	}
	// Record the challenge (log-safe fields only — never the matched value).
	s.riskScanner.RecordPolicyChallenge(ctx, ev.Context.OrganizationID, ev.Context.ProjectID, ev.Context.User.ID, scanResult.PolicyID, toolName, scanResult.PolicyName, scanResult.Entity, scanResult.RuleID)

	var toolPtr *string
	if toolName != "" {
		toolPtr = &toolName
	}
	ackURL, _, err := risk.GeneratePolicyAckURL(ctx, s.cache, s.siteURL, risk.PolicyAckTokenInput{
		OrganizationID: ev.Context.OrganizationID,
		ProjectID:      ev.Context.ProjectID.String(),
		UserID:         ev.Context.User.ID,
		RiskPolicyID:   scanResult.PolicyID,
		PolicyName:     scanResult.PolicyName,
		ToolName:       toolPtr,
	}, 0)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate risk policy ack link; falling back to block",
			attr.SlogError(err))
		return "", false
	}
	return renderWarnReason(scanResult, ackURL), true
}

// renderWarnBody builds the challenge message body. Uses the policy's
// user_message as a template (placeholders %{match} %{entity} %{policy} %{rule})
// when set, else a safe default. It contains NO acknowledgement link — callers
// that need the out-of-band ack flow wrap it via renderWarnReason; transports
// with a native confirmation prompt (Claude PreToolUse "ask") use the body
// directly.
func renderWarnBody(scanResult *risk.ScanResult) string {
	match := truncateForWarn(scanResult.MatchedValue)
	entity := scanResult.Entity
	if entity == "" {
		entity = scanResult.RuleID
	}

	if scanResult.UserMessage != nil && strings.TrimSpace(*scanResult.UserMessage) != "" {
		r := strings.NewReplacer(
			"%{match}", match,
			"%{entity}", entity,
			"%{policy}", scanResult.PolicyName,
			"%{rule}", scanResult.RuleID,
		)
		return r.Replace(*scanResult.UserMessage)
	}
	if match != "" {
		return fmt.Sprintf("Your request matched policy %q: potentially harmful or sensitive content %q identified as %s. Do you wish to continue?",
			scanResult.PolicyName, match, entity)
	}
	return fmt.Sprintf("Your request matched policy %q: potentially harmful or sensitive content identified as %s. Do you wish to continue?",
		scanResult.PolicyName, entity)
}

// renderWarnReason builds the challenge body plus the acknowledgement
// instructions + link, for transports without a native confirmation prompt
// (the out-of-band ack fallback).
func renderWarnReason(scanResult *risk.ScanResult, ackURL string) string {
	return renderWarnBody(scanResult) + "\n\nAcknowledge to proceed, then ask me to retry the request:\n" + ackURL
}

// recordWarnChallenge best-effort records that a warn (challenge) was surfaced
// to the user, using log-safe fields only (never the matched value). Safe to
// call on any transport; a no-op when the scanner or user id is absent.
func (s *Service) recordWarnChallenge(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult, toolName string) {
	if s.riskScanner == nil || scanResult == nil || ev.Context.User.ID == "" {
		return
	}
	s.riskScanner.RecordPolicyChallenge(ctx, ev.Context.OrganizationID, ev.Context.ProjectID, ev.Context.User.ID, scanResult.PolicyID, toolName, scanResult.PolicyName, scanResult.Entity, scanResult.RuleID)
}

// ANSI amber (256-color 214) + bold, to make the challenge stand out in the
// Claude Code permission prompt. Terminals that honor ANSI render it amber;
// clients that strip escapes still get the plain text + ⚠ marker.
const (
	ansiAmberBold = "\x1b[1;38;5;214m"
	ansiReset     = "\x1b[0m"
)

// emphasizeWarn makes a challenge message stand out (amber + ⚠) for transports
// whose confirmation UI renders the reason without its own emphasis.
func emphasizeWarn(body string) string {
	return "⚠ " + ansiAmberBold + body + ansiReset
}

func truncateForWarn(v string) string {
	if len([]rune(v)) <= warnMatchMaxLen {
		return v
	}
	return string([]rune(v)[:warnMatchMaxLen]) + "…"
}
