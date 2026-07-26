package policies

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// ToolScanner runs the MCP- and plain-flavored enforcement risk scans over a
// tool request. The two flavors exist because saved CEL expressions key on
// the event type; the stage picks the flavor with the ingest path's own MCP
// predicate so expressions evaluate exactly as the inline evaluation ran
// them.
type ToolScanner interface {
	ScanMCPToolRequest(ctx context.Context, req *Request, actor Actor, toolName string, toolInput any, at time.Time) *risk.ScanResult
	ScanToolRequest(ctx context.Context, req *Request, actor Actor, toolName string, toolInput any, at time.Time) *risk.ScanResult
}

// BlockPageLinker mints the durable block row for a policy-denied tool call
// and appends its block-page URL to the user-facing reason, returning the
// reason unchanged when no row can be minted (retried deliveries, missing
// site URL). It exists so the stages own the deny wording while the hooks
// service owns the block-row persistence.
type BlockPageLinker interface {
	AppendBlockPageURL(ctx context.Context, req *Request, actor Actor, auditReason, toolName, policyID, userReason string) string
}

// RiskScanToolPreGate builds the policy that scans a tool request against
// the org's risk policies, routing through the MCP-flavored event exactly as
// the inline evaluation did so saved CEL expressions evaluate identically.
func RiskScanToolPreGate(scans ToolScanner, links BlockPageLinker, warns WarnChallenger) func(context.Context, *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		return riskScanToolRequest(ctx, scans, links, warns, &ev.Event, ev.Tool)
	}
}

// riskScanToolRequest is the shared MCP-or-plain risk scan behind
// RiskScanToolPreGate and RiskScanPermissionToolGate, with the "tool call"
// deny wording both inline branches used. The tool projections come off the
// event: the name verbatim, the input via toolInputOf, and the scan flavor
// from the stamped MCP predicate.
func riskScanToolRequest(ctx context.Context, scans ToolScanner, links BlockPageLinker, warns WarnChallenger, env *agenthooks.Event, tool agenthooks.ToolCall) (agenthooks.ToolPreDecision, error) {
	req := RequestFromContext(ctx)
	if req == nil {
		return agenthooks.NoDecision(), nil
	}
	actor := ActorFromContext(ctx)
	input := toolInputOf(tool)
	var scan *risk.ScanResult
	if mcpToolRequest(env) {
		scan = scans.ScanMCPToolRequest(ctx, req, actor, tool.Name, input, env.Time)
	} else {
		scan = scans.ScanToolRequest(ctx, req, actor, tool.Name, input, env.Time)
	}
	return riskScanToolDecision(ctx, links, warns, req, actor, scan, tool.Name, "tool call", env.Time)
}

// riskScanToolDecision translates a tool-flavored scan result into a
// decision. nil stays neutral. A warn (challenge) first checks for a live
// acknowledgement: an acknowledged warn stays neutral so evaluation falls
// through to the remaining stages — the shadow-MCP guard after a tool scan,
// the MCP-or-tool re-scan after a permission scan — never short-circuiting
// the call (mirrors the Claude PreToolUse handler). An unacknowledged warn
// denies with the challenge framing and the ack link; when no ack link can
// be produced it falls back to a plain block (fail-safe — a warn must never
// silently allow). A block becomes a deny carrying the audit reason for the
// model and a user-facing message for the human: the policy's custom user
// message when set, else the audit reason (WithBlockReason picks the first
// non-empty candidate), with the durable block-page URL appended for live
// deliveries.
func riskScanToolDecision(ctx context.Context, links BlockPageLinker, warns WarnChallenger, req *Request, actor Actor, scan *risk.ScanResult, toolName, blockedWhat string, eventTime time.Time) (agenthooks.ToolPreDecision, error) {
	if scan == nil {
		return agenthooks.NoDecision(), nil
	}
	if scan.Action == "warn" {
		if warns.WarnAcknowledged(ctx, req, actor, scan, toolName, eventTime) {
			return agenthooks.NoDecision(), nil
		}
		if _, userReason, ok := warns.WarnDenyReason(ctx, req, actor, scan, toolName, eventTime); ok {
			auditReason := fmt.Sprintf("Speakeasy challenged this %s: matched policy %q (%s)", blockedWhat, scan.PolicyName, scan.Description)
			return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
		}
	}
	auditReason := fmt.Sprintf("Speakeasy blocked this %s: matched policy %q (%s)", blockedWhat, scan.PolicyName, scan.Description)
	decision := agenthooks.Deny(auditReason).WithBlockReason(conv.PtrValOr(scan.UserMessage, ""), auditReason)
	userReason := links.AppendBlockPageURL(ctx, req, actor, auditReason, toolName, scan.PolicyID, decision.SystemMessage())
	return decision.WithSystemMessage(userReason), nil
}
