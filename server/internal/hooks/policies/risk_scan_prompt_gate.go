package policies

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// PromptScanFunc runs the prompt-flavored enforcement risk scan (CEL
// policies) over a submitted prompt. A nil result means no policy matched or
// the scan could not run (the primitive swallows its failures — log + fail
// open). The hooks Enforcer's ScanPrompt has this shape.
type PromptScanFunc func(ctx context.Context, req *Request, actor Actor, prompt string, at time.Time) *risk.ScanResult

// RiskScanPromptGate builds the policy that scans a submitted prompt against
// the org's risk policies. A warn (challenge) holds the prompt for
// out-of-band acknowledgement when the ingest surface allows it
// (Request.AllowWarnAck): an acknowledged challenge stays neutral so the
// prompt goes through, an unacknowledged one denies with the ack link, and
// when no ack link can be produced the warn falls back to a plain block
// (fail-safe — a warn must never silently allow).
func RiskScanPromptGate(scanPrompt PromptScanFunc, warnAcknowledged WarnAckFunc, warnDenyReason WarnDenyFunc) func(context.Context, *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
		var neutral agenthooks.PromptDecision
		req := RequestFromContext(ctx)
		if req == nil {
			return neutral, nil
		}
		actor := ActorFromContext(ctx)
		scan := scanPrompt(ctx, req, actor, ev.Prompt, ev.Time)
		if scan == nil {
			return neutral, nil
		}
		if scan.Action == "warn" && req.AllowWarnAck {
			if warnAcknowledged(ctx, req, actor, scan, "", ev.Time) {
				return neutral, nil
			}
			if _, userReason, ok := warnDenyReason(ctx, req, actor, scan, "", ev.Time); ok {
				auditReason := fmt.Sprintf("Speakeasy challenged this prompt: matched policy %q (%s)", scan.PolicyName, scan.Description)
				return agenthooks.BlockPrompt(auditReason).WithSystemMessage(userReason), nil
			}
		}
		auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scan.PolicyName, scan.Description)
		return agenthooks.BlockPrompt(auditReason).
			WithBlockReason(conv.PtrValOr(scan.UserMessage, ""), auditReason), nil
	}
}
