package hooks

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// riskScanTrackerKey carries a flag the transport handlers install before
// dispatching an event, flipped when an enforcement scan actually executes
// (rather than short-circuiting on a guard). It feeds the risk_scanned
// dimension on hooks.event.duration so gating latency that includes a scan is
// separable from the no-scan baseline. The flag is atomic because a legacy
// Claude handler can still be scanning when its request records the metric.
type riskScanTrackerKey struct{}

func withRiskScanTracker(ctx context.Context) (context.Context, *atomic.Bool) {
	scanned := new(atomic.Bool)
	return context.WithValue(ctx, riskScanTrackerKey{}, scanned), scanned
}

func markRiskScanned(ctx context.Context) {
	if scanned, ok := ctx.Value(riskScanTrackerKey{}).(*atomic.Bool); ok {
		scanned.Store(true)
	}
}

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
	ctx, span := s.tracer.Start(ctx, "hooks.scanHookEventForEnforcement")
	defer span.End()

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

	markRiskScanned(ctx)
	toolCallID := ""
	if messageType == message.ToolRequest {
		toolCallID = hookEventToolCallID(ev, toolName)
	}
	// Capture runs independently of enforcement; resolving a canonical message
	// here would add a database round trip before the safety decision.
	result, err := s.riskScanner.ScanForEnforcement(ctx, risk.RealtimeScanRequest{
		Provenance: metering.RiskProvenance{
			OrganizationID:         ev.Context.OrganizationID,
			ProjectID:              ev.Context.ProjectID,
			RiskPolicyID:           uuid.Nil,
			RiskPolicyVersion:      0,
			PolicyLinkReason:       "",
			ChatID:                 uuid.Nil,
			ExternalConversationID: ev.ConversationID,
			ChatMessageID:          uuid.Nil,
			ContentPartID:          uuid.Nil,
			MessageLinkReason:      "realtime_message_not_resolved",
			OperationID:            hookRiskOperationID(ev, messageType, toolName),
			ExecutionPath:          "realtime_local",
			RequestID:              "",
			MessageType:            messageType,
			HookSource:             string(ev.Provider),
			UserID:                 ev.Context.User.ID,
			ToolCallID:             toolCallID,
			ToolName:               toolName,
			Model:                  "",
			Provider:               "",
		},
		Text:        text,
		MessageType: messageType,
		ToolName:    toolName,
		ToolCallID:  toolCallID,
	})
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
func hookRiskOperationID(ev hookevents.Event, messageType message.Type, toolName string) string {
	var token string
	switch payload := ev.Raw.(type) {
	case *gen.ClaudePayload:
		token = conv.PtrValOr(payload.IdempotencyKey, "")
	case *gen.CursorPayload:
		token = conv.PtrValOr(payload.IdempotencyKey, "")
	case *gen.CodexPayload:
		token = conv.PtrValOr(payload.IdempotencyKey, "")
	case *gen.IngestPayload:
		token = conv.PtrValOr(payload.IdempotencyKey, "")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		// Match hook ingestion: without a sender token, identical content
		// cannot distinguish a new invocation from a redelivery.
		return uuid.NewString()
	}
	projectID := ev.Context.ProjectID.String()
	identity := fmt.Sprintf(
		"%d:%s%d:%s%d:%s%d:%s%d:%s%d:%s%d:%s",
		len(ev.Context.OrganizationID), ev.Context.OrganizationID,
		len(projectID), projectID,
		len(ev.Provider), ev.Provider,
		len(ev.RawEventType), ev.RawEventType,
		len(messageType), messageType,
		len(toolName), toolName,
		len(token), token,
	)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:realtime:"+identity)).String()
}

func hookEventToolCallID(ev hookevents.Event, toolName string) string {
	switch payload := ev.Raw.(type) {
	case *gen.ClaudePayload:
		return conv.PtrValOr(payload.ToolUseID, "")
	case *gen.CursorPayload:
		return cursorToolCorrelationID(payload)
	case *gen.CodexPayload:
		return syntheticToolCallID(ev.ConversationID, toolName)
	case *gen.IngestPayload:
		return canonicalChatToolCallID(payload)
	default:
		return ""
	}
}

// renderUserBlockReason returns the message shown to the agent when a tool
// call or prompt is denied. When the policy carries a non-empty user_message,
// that overrides the default Speakeasy-branded format; otherwise the supplied
// audit reason is rendered verbatim. The audit reason itself is what gets
// stored in ClickHouse traces — the user_message override only affects what
// the agent / end user sees.
//
// A fail-closed deny from the LLM analyzer lane is not a policy match, so it
// renders the "analysis unavailable" copy instead: the policy's user_message
// describes a violation that did not happen, and the audit reason carries the
// technical classification that operators, not agents, need.
func renderUserBlockReason(scanResult *risk.ScanResult, auditReason string) string {
	if scanResult.AnalysisUnavailable() {
		return analysisUnavailableReason(scanResult)
	}
	if scanResult.UserMessage != nil && strings.TrimSpace(*scanResult.UserMessage) != "" {
		return renderPolicyPlaceholders(*scanResult.UserMessage, scanResult, maskBlockedMatch(scanResult.MatchedValue))
	}
	return auditReason
}

// maskBlockedMatch is %{match} in a block message. A block exists to keep the
// value out of the session, so the message only hints at it: up to four
// characters from each end, and never more than a quarter of the value, so
// the person can tell which value was caught. A value of 12 characters or
// fewer shows as "…". A match with no value (an LLM judge finding) renders
// as an empty string rather than suggest a hidden value. Repeating a live
// credential in full also makes Claude's apps drop the whole message.
func maskBlockedMatch(v string) string {
	r := []rune(v)
	if len(r) == 0 {
		return ""
	}
	if len(r) <= 12 {
		return "…"
	}
	show := min(4, len(r)/8)
	return string(r[:show]) + "…" + string(r[len(r)-show:])
}

// shadowMCPEntity is %{entity} in a shadow-MCP block message.
const shadowMCPEntity = "shadow MCP server"

// renderPolicyUserMessage applies a policy's user_message override to a deny
// that has no ScanResult behind it (the shadow-MCP guard). The template takes
// the same placeholders as other block messages: %{match} is the observed
// server, %{policy} the policy name, and %{rule} is empty. The audit reason
// is rendered verbatim when there is no override.
func renderPolicyUserMessage(userMessage *string, auditReason, policyName, server string) string {
	if userMessage == nil || strings.TrimSpace(*userMessage) == "" {
		return auditReason
	}
	return fillPolicyPlaceholders(*userMessage, server, shadowMCPEntity, policyName, "")
}

// analysisUnavailableReason is the user-facing deny copy for a fail-closed
// result: the LLM analyzer could not be consulted, so the policy denied
// without a finding.
func analysisUnavailableReason(scanResult *risk.ScanResult) string {
	return fmt.Sprintf("Risk analysis is temporarily unavailable; this action was denied by policy %q.", scanResult.PolicyName)
}

// warnMatchMaxLen bounds the matched value shown in a challenge warning. The
// match is sensitive; it appears ONLY in this ephemeral agent-facing message,
// never in logs, ClickHouse, tool_call_blocks, or audit.
const warnMatchMaxLen = 120

// warnAcknowledged reports whether the user has a live acknowledgement for a
// warn (challenge) match, so the retried call should be allowed. Only meaningful
// when scanResult.Action == "confirm".
func (s *Service) warnAcknowledged(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult, toolName string) bool {
	ctx, span := s.tracer.Start(ctx, "hooks.warnAcknowledged")
	defer span.End()

	if s.riskScanner == nil || scanResult == nil {
		return false
	}
	return s.riskScanner.HasAcknowledgedChallenge(ctx, ev.Context.ProjectID, ev.Context.User.ID, scanResult.PolicyID, toolName, scanResult.CallFingerprint)
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
	ctx, span := s.tracer.Start(ctx, "hooks.warnDenyReason")
	defer span.End()

	if s.cache == nil || ev.Context.User.ID == "" {
		return "", "", false
	}
	siteURL := s.orgSiteURL(ctx, ev.Context.OrganizationID)
	if siteURL == nil {
		return "", "", false
	}
	// Record the challenge (log-safe fields only — never the matched value).
	s.riskScanner.RecordPolicyChallenge(ctx, ev.Context.OrganizationID, ev.Context.ProjectID, ev.Context.User.ID, scanResult.PolicyID, toolName, scanResult.PolicyName, scanResult.Entity, scanResult.RuleID, scanResult.CallFingerprint)

	ackURL, _, err := risk.GeneratePolicyAckURL(ctx, s.cache, siteURL, policyAckInput(ev, scanResult, toolName), 0)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate risk policy ack link; falling back to block",
			attr.SlogError(err))
		return "", "", false
	}
	return renderWarnAgentReason(scanResult), renderWarnUserReason(scanResult, ackURL), true
}

// policyAckInput is the challenge state an acknowledgement link redeems.
func policyAckInput(ev hookevents.Event, scanResult *risk.ScanResult, toolName string) risk.PolicyAckTokenInput {
	var toolPtr *string
	if toolName != "" {
		toolPtr = &toolName
	}
	return risk.PolicyAckTokenInput{
		OrganizationID: ev.Context.OrganizationID,
		ProjectID:      ev.Context.ProjectID.String(),
		UserID:         ev.Context.User.ID,
		RiskPolicyID:   scanResult.PolicyID,
		PolicyName:     scanResult.PolicyName,
		ToolName:       toolPtr,
		// Scope the ack to this concrete call.
		CallFingerprint: scanResult.CallFingerprint,
		// Human-facing challenge shown on the approval page (may include the
		// match — ephemeral, token-gated, same as the terminal display).
		ChallengeMessage: renderWarnBody(scanResult),
		// Zero: use the ack-window default (defaultAckGrace) so the retry passes.
		RememberFor: 0,
	}
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
		return renderPolicyPlaceholders(*scanResult.UserMessage, scanResult, match)
	}
	if match != "" {
		return fmt.Sprintf("Your request matched policy %q: potentially harmful or sensitive content %q identified as %s. Do you wish to continue?",
			scanResult.PolicyName, match, entity)
	}
	return fmt.Sprintf("Your request matched policy %q: potentially harmful or sensitive content identified as %s. Do you wish to continue?",
		scanResult.PolicyName, entity)
}

// renderPolicyPlaceholders fills a policy's custom message template:
// %{match} (the caller's rendering of the matched value), %{entity},
// %{policy} and %{rule}. Warn, block and quarantine messages all accept the
// same placeholders.
func renderPolicyPlaceholders(message string, scanResult *risk.ScanResult, match string) string {
	entity := scanResult.Entity
	if entity == "" {
		entity = scanResult.RuleID
	}
	return fillPolicyPlaceholders(message, match, entity, scanResult.PolicyName, scanResult.RuleID)
}

func fillPolicyPlaceholders(message, match, entity, policy, rule string) string {
	return strings.NewReplacer(
		"%{match}", match,
		"%{entity}", entity,
		"%{policy}", policy,
		"%{rule}", rule,
	).Replace(message)
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
			"or its input. Only the human user can confirm it, out of band. Stop here, "+
			"tell the user this action needs their confirmation, and wait for them to "+
			"confirm it and explicitly ask you to retry. Do not attempt to bypass it, do "+
			"not fetch any URL yourself, and do not confirm it on the user's behalf.",
		scanResult.PolicyName)
}

// renderWarnUserReason is the human-facing framing: the policy message plus the
// acknowledgement link the operator opens to approve.
func renderWarnUserReason(scanResult *risk.ScanResult, ackURL string) string {
	return renderWarnBody(scanResult) + fmt.Sprintf(
		"\n\nSpeakeasy risk policy %q blocked this action until you confirm it. To go "+
			"ahead, open this link and confirm, then tell the agent to retry:\n%s",
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
