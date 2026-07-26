package policies

import (
	"context"
	"time"

	"github.com/speakeasy-api/agenthooks"
)

// SpendCheckFunc consults the spend-rule circuit for the actor on a hook
// event and renders the deny reason. It owns the whole spend decision —
// which adapters are gated, the circuit lookup, and the reason wording — so
// the stages only translate a blocked verdict into a decision. blocked=false
// covers every non-deny outcome: an ungated adapter, an under-budget actor,
// and the primitive's fail-open error paths. The hooks Enforcer's CheckSpend
// has this shape.
type SpendCheckFunc func(ctx context.Context, req *Request, actor Actor, kind string, at time.Time) (auditReason string, blocked bool)

// SpendGatePrompt builds the policy that denies a submitted prompt for an
// over-budget actor. It runs before any risk-policy evaluation — exactly
// where the inline evaluation ran the spend gate — for every adapter with a
// per-provider enforcement surface (claude, codex, cursor); opencode still
// passes through untouched pending a product decision on its enforcement
// surface.
func SpendGatePrompt(checkSpend SpendCheckFunc) func(context.Context, *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
		var neutral agenthooks.PromptDecision
		req := RequestFromContext(ctx)
		if req == nil {
			return neutral, nil
		}
		auditReason, blocked := checkSpend(ctx, req, ActorFromContext(ctx), "prompt", ev.Time)
		if !blocked {
			return neutral, nil
		}
		return agenthooks.BlockPrompt(auditReason).WithSystemMessage(auditReason), nil
	}
}

// SpendGateToolPre builds the tool.requested spend gate. A deny mints the
// durable block row and appends its block-page URL to the user-facing
// reason, matching the risk-scan deny shape on this path.
func SpendGateToolPre(checkSpend SpendCheckFunc, appendBlockURL BlockPageLinkFunc) func(context.Context, *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		return spendGateToolRequest(ctx, checkSpend, appendBlockURL, "tool call", ev.Tool.Name, ev.Time)
	}
}

// SpendGatePermission builds the permission.request sibling of
// SpendGateToolPre. Permission-shaped tool.requested events keep the
// permission framing, matching this path's risk wording and the legacy
// codex endpoint's spend deny.
func SpendGatePermission(checkSpend SpendCheckFunc, appendBlockURL BlockPageLinkFunc) func(context.Context, *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
		return spendGateToolRequest(ctx, checkSpend, appendBlockURL, "permission request", ev.Tool.Name, ev.Time)
	}
}

// spendGateToolRequest is the shared tool-flavored spend gate behind
// SpendGateToolPre and SpendGatePermission. The tool name comes off the
// event, matching the risk-scan gates.
func spendGateToolRequest(ctx context.Context, checkSpend SpendCheckFunc, appendBlockURL BlockPageLinkFunc, kind, toolName string, eventTime time.Time) (agenthooks.ToolPreDecision, error) {
	req := RequestFromContext(ctx)
	if req == nil {
		return agenthooks.NoDecision(), nil
	}
	actor := ActorFromContext(ctx)
	auditReason, blocked := checkSpend(ctx, req, actor, kind, eventTime)
	if !blocked {
		return agenthooks.NoDecision(), nil
	}
	userReason := appendBlockURL(ctx, req, actor, auditReason, toolName, "", auditReason)
	return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
}
