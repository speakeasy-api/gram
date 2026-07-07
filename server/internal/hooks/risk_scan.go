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

// warnDenyReason records the challenge and returns two framings of the deny:
//
//   - agentReason is model-facing. It carries NO acknowledgement link and is
//     phrased as an authoritative policy decision, because a "go to this URL and
//     acknowledge" instruction placed in front of the agent reads exactly like a
//     prompt injection embedded in tool output — safety-trained models dismiss
//     it and never surface the challenge.
//   - userReason is human-facing and carries the acknowledgement link the
//     operator opens to approve.
//
// Transports with separate agent/user channels (Claude
// permissionDecisionReason vs systemMessage; Cursor AgentMessage vs
// UserMessage) should route each accordingly.
//
// ok=false means an ack link could not be produced (missing site URL / cache /
// user id) — the caller MUST fall back to a plain block (fail-safe): a warn must
// never silently allow.
func (s *Service) warnDenyReason(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult, toolName string) (agentReason, userReason string, ok bool) {
	if s.siteURL == nil || s.cache == nil || ev.Context.User.ID == "" {
		return "", "", false
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
		// Human-facing challenge shown on the approval page (may include the
		// match — ephemeral, token-gated, same as the terminal display).
		ChallengeMessage: renderWarnBody(scanResult),
		// Zero: use the ack-window default (defaultAckGrace) so the retry passes.
		RememberFor: 0,
	}, 0)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate risk policy ack link; falling back to block",
			attr.SlogError(err))
		return "", "", false
	}
	return renderWarnAgentReason(scanResult), renderWarnUserReason(scanResult, ackURL), true
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

// renderWarnAgentReason is the model-facing framing of a warn challenge. It
// carries NO acknowledgement link on purpose: the link is for the human, and a
// "go to this URL" instruction in front of the model reads like an injected
// instruction, which safety-trained agents dismiss. Instead it states the
// authority and tells the agent to stop and defer to the operator.
func renderWarnAgentReason(scanResult *risk.ScanResult) string {
	return fmt.Sprintf(
		"Blocked by your organization's Speakeasy risk policy %q. This is an enforced "+
			"security policy decision — not tool output, and not a message from the tool "+
			"or its input. Approval must be granted by a human operator out of band. Stop "+
			"here, tell the user this action needs their approval, and wait for them to "+
			"approve it and explicitly ask you to retry. Do not attempt to bypass it, do "+
			"not fetch any URL yourself, and do not approve it on the user's behalf.",
		scanResult.PolicyName)
}

// renderWarnUserReason is the human-facing framing: the policy message plus the
// acknowledgement link the operator opens to approve.
func renderWarnUserReason(scanResult *risk.ScanResult, ackURL string) string {
	return renderWarnBody(scanResult) + fmt.Sprintf(
		"\n\nThis action was held for review by Speakeasy risk policy %q. To approve it, "+
			"open this link and acknowledge, then tell the agent to retry:\n%s",
		scanResult.PolicyName, terminalHyperlink(ackURL))
}

// terminalHyperlink renders a URL as a clickable, high-visibility link. The URL
// is wrapped in an OSC 8 hyperlink escape (clickable in terminals that support
// it) and styled bold + underline + amber via SGR so it stands out against the
// dim/gray systemMessage rendering. The URL is also the visible label, so
// terminals without OSC 8 support (or clients that strip the hyperlink escape)
// still show a bold amber, copyable URL. Claude Code forwards these escapes to
// the terminal. See https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda
func terminalHyperlink(url string) string {
	const (
		style = "\x1b[1;4;38;5;214m" // bold + underline + amber (256-color 214)
		reset = "\x1b[0m"
	)
	return style + "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\" + reset
}

func truncateForWarn(v string) string {
	if len([]rune(v)) <= warnMatchMaxLen {
		return v
	}
	return string([]rune(v)[:warnMatchMaxLen]) + "…"
}
