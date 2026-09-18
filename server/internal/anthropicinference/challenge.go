package anthropicinference

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// A warn policy is a challenge, not a verdict: the warned human is supposed to
// be able to approve the action and retry. Every other transport carries that
// affordance on a side channel (Claude Code's systemMessage, Cursor's
// UserMessage). This protocol has none — a single `deny_reason` string, capped
// at 500 characters, is everything the user ever sees — so a warn used to reach
// them as an unappealable block with nowhere to go. The acknowledgement link
// therefore travels inside `deny_reason` itself.
//
// Putting an "open this URL" instruction in front of a model is what the hook
// transports avoid, because it reads like an injected instruction. That concern
// does not apply here: inference hooks deny *before* inference, so the model
// never sees this text. The end user does.
//
// The link redeems the same rpak1 token the hook transports mint, against the
// same acknowledgement record, so approving here behaves exactly as it does
// elsewhere: it clears only an identical retry of this concrete call, by this
// user, within the ack window.

const (
	// maxDenyReasonRunes mirrors the protocol bound the handler enforces. The
	// handler truncates from the right, which would amputate a trailing URL, so
	// the copy is budgeted here instead of relying on that cut.
	maxDenyReasonRunes = 500

	defaultDenyReason = "This request was blocked by your organization's security policy."

	ackInstruction = "Held for review. To approve this and retry, open:\n"
)

// warnAcknowledged reports whether this user already approved this exact call.
// The retry arrives as an ordinary delivery carrying no token, so this lookup —
// not the link — is what lets it through. Fail-closed on every error.
func (s *Service) warnAcknowledged(ctx context.Context, config Config, userID string, result *risk.ScanResult, toolName string) bool {
	if s.scanner == nil || userID == "" {
		return false
	}
	return s.scanner.HasAcknowledgedChallenge(ctx, config.ProjectID, userID, result.PolicyID, toolName, result.CallFingerprint)
}

// warnChallengeReason records the challenge and renders deny copy carrying the
// acknowledgement link. ok=false means no link could be minted (unresolved
// user, no cache, no site URL); the caller must then fall back to a plain deny,
// because a warn must never silently allow.
func (s *Service) warnChallengeReason(ctx context.Context, config Config, userID string, result *risk.ScanResult, toolName string) (string, bool) {
	if s.scanner == nil || s.cache == nil || s.siteURL == nil || userID == "" {
		return "", false
	}

	// Log-safe fields only — never the matched value.
	s.scanner.RecordPolicyChallenge(ctx, config.OrganizationID, config.ProjectID, userID, result.PolicyID, toolName, result.PolicyName, result.Entity, result.RuleID, result.CallFingerprint)

	body := denyBody(result)
	var toolPtr *string
	if toolName != "" {
		toolPtr = &toolName
	}
	ackURL, _, err := risk.GeneratePolicyAckURL(ctx, s.cache, s.siteURL, risk.PolicyAckTokenInput{
		OrganizationID: config.OrganizationID,
		ProjectID:      config.ProjectID.String(),
		UserID:         userID,
		RiskPolicyID:   result.PolicyID,
		PolicyName:     result.PolicyName,
		ToolName:       toolPtr,
		// Scope the acknowledgement to this concrete message or tool call.
		CallFingerprint: result.CallFingerprint,
		// The approval page repeats the copy the user saw in Claude so the two
		// surfaces describe the same decision.
		ChallengeMessage: body,
		// Zero: use the ack-window default, which covers the retry.
		RememberFor: 0,
	}, 0)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate risk policy ack link for inference hook; falling back to a plain deny",
			attr.SlogError(err), attr.SlogRiskPolicyID(result.PolicyID))
		return "", false
	}

	return renderChallengeDenyReason(body, ackURL), true
}

// denyBody is the policy's own copy: its configured user_message, or a generic
// fallback when it has none.
func denyBody(result *risk.ScanResult) string {
	if result.UserMessage != nil {
		if message := strings.TrimSpace(*result.UserMessage); message != "" {
			return message
		}
	}
	return defaultDenyReason
}

// renderChallengeDenyReason appends the approval link to the policy's copy,
// reserving the link's room first so a long user_message cannot push it past
// the protocol's 500-character bound.
func renderChallengeDenyReason(body, ackURL string) string {
	suffix := "\n\n" + ackInstruction + ackURL
	budget := maxDenyReasonRunes - len([]rune(suffix))
	if budget <= 0 {
		// A site URL long enough to leave no room for copy is pathological, but
		// the link is the part the user cannot reconstruct: keep it.
		return strings.TrimSpace(suffix)
	}
	return truncateRunes(body, budget) + suffix
}

func truncateRunes(v string, limit int) string {
	runes := []rune(v)
	if len(runes) <= limit {
		return v
	}
	return string(runes[:limit-1]) + "…"
}
