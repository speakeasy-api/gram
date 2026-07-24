package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/agenthooks"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// This file is the router-backed ingest decision layer: the policy Runner
// built once per Service, its middleware, and the policy stages. The stages
// are thin adapters over the existing enforcement primitives (risk scans,
// shadow-MCP evaluation, block-page URL minting) — they translate the
// primitives' outcomes into agenthooks decisions and nothing else, so the
// enforcement behavior is exactly the inline evaluation Ingest ran before.
// The boundary that maps the winning decision back onto the wire response
// lives in evaluateCanonicalHook (ingest_hooks.go).

// ingestPolicyRequest carries the per-request ingest state the policy stages
// need beyond the typed event: the validated canonical payload (the typed
// events project only the decision-relevant fields), the authenticated
// context, and the actor Ingest resolved for attribution. It rides on ctx so
// stages keep the plain agenthooks handler signatures.
type ingestPolicyRequest struct {
	payload *gen.IngestPayload
	authCtx *contextvalues.AuthContext
	actor   canonicalActor
}

type ingestPolicyRequestKey struct{}

func withIngestPolicyRequest(ctx context.Context, req *ingestPolicyRequest) context.Context {
	return context.WithValue(ctx, ingestPolicyRequestKey{}, req)
}

func ingestPolicyRequestFrom(ctx context.Context) *ingestPolicyRequest {
	req, _ := ctx.Value(ingestPolicyRequestKey{}).(*ingestPolicyRequest)
	return req
}

// policyActorKey carries the resolved actor for the gating stages. Only the
// actorResolution middleware writes it: stages depend on "the actor is in
// ctx", not on how it got there.
type policyActorKey struct{}

func policyActor(ctx context.Context) canonicalActor {
	actor, _ := ctx.Value(policyActorKey{}).(canonicalActor)
	return actor
}

// newPolicyRunner builds the ingest decision pipeline once per Service. The
// registration block below IS the run order, and the order replicates the
// old inline evaluateCanonicalHook exactly:
//
//   - actorResolution middleware makes the resolved actor available to every
//     gating stage.
//   - the spend gate runs first on every gated kind: inline it ran before
//     any risk-policy evaluation, so an over-budget actor is denied before
//     any policy scan.
//   - prompt.submitted: the spend gate, then the risk scan (CEL policies).
//   - tool.requested: the spend gate, then the risk scan (MCP- or
//     tool-flavored, matching the request shape), then the shadow-MCP gate
//     (deny + bypass-request link for non-Gram MCP servers). The risk scan
//     ran first inline too, so a risk block wins over a shadow deny and the
//     risk_scanned metric dimension is set even when the shadow gate would
//     deny.
//   - tool.requested with permission_type (dispatched as permission.request):
//     the spend gate (with the permission framing), the permission-flavored
//     risk scan, then the same MCP-or-tool risk scan the inline code fell
//     through to (a deliberate duplicate scan of the same request — see
//     riskScanPermissionToolGate), then the shadow gate.
//
// Stages are method values on the Service so they read dependencies
// (riskScanner, spendGate, shadowMCPClient, siteURL, ...) at call time —
// tests swap those fields after construction. Walk reports the reflected
// method names; the snapshot test pins them.
func (s *Service) newPolicyRunner() *agenthooks.Runner {
	r := agenthooks.New(agenthooks.WithLogger(s.logger))

	r.Use(s.actorResolution)

	r.OnPromptSubmitted(
		s.spendGatePromptGate,
		s.riskScanPromptGate,
	)
	r.OnToolPre(
		s.spendGateToolPreGate,
		s.riskScanToolPreGate,
		s.shadowMCPToolPreGate,
	)
	r.OnPermission(
		s.spendGatePermissionGate,
		s.riskScanPermissionGate,
		s.riskScanPermissionToolGate,
		s.shadowMCPPermissionGate,
	)

	return r
}

// actorResolution stashes the request's resolved actor in ctx for the stages
// below. The resolution itself is shared with the rest of Ingest: the actor
// is resolved exactly once per request (resolveCanonicalActor, including the
// session-metadata cache fallback) and carried in the policy request, so
// enforcement and persistence can never disagree on the identity — resolving
// again here could diverge when the session cache changes between reads.
func (s *Service) actorResolution(ctx context.Context, typed any, next agenthooks.Next) (agenthooks.Decision, error) {
	if req := ingestPolicyRequestFrom(ctx); req != nil {
		ctx = context.WithValue(ctx, policyActorKey{}, req.actor)
	}
	return next(ctx, typed)
}

// spendGatePromptGate / spendGateToolPreGate / spendGatePermissionGate deny
// events for over-budget actors before any risk-policy evaluation — exactly
// where the inline evaluation ran the spend gate. The gate covers every
// adapter with a per-provider enforcement surface (claude, codex, cursor);
// the risk scans below run adapter-agnostically, and an over-budget actor is
// over budget regardless of which agent carries the event. Adapters are
// self-reported slugs, so this remains a cooperative-client boundary like
// the rest of the ingest surface; matching is case-insensitive so a case
// variant cannot dodge the gate. opencode still passes through untouched
// pending a product decision on its enforcement surface.
func (s *Service) spendGatePromptGate(ctx context.Context, ev *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
	var neutral agenthooks.PromptDecision
	req := ingestPolicyRequestFrom(ctx)
	if req == nil || !spendGatedAdapter(req.payload.Source.Adapter) {
		return neutral, nil
	}
	event := canonicalHookEvent(req.payload, req.authCtx, policyActor(ctx), ev.Time)
	block := s.checkSpendGate(ctx, event)
	if block == nil {
		return neutral, nil
	}
	auditReason := spendBlockReason("prompt", block)
	return agenthooks.BlockPrompt(auditReason).WithSystemMessage(auditReason), nil
}

func (s *Service) spendGateToolPreGate(ctx context.Context, ev *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return s.spendGateToolRequest(ctx, "tool call", ev.Time)
}

// spendGatePermissionGate keeps the permission framing for permission-shaped
// tool.requested events, matching this path's risk wording and the legacy
// codex endpoint's spend deny.
func (s *Service) spendGatePermissionGate(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return s.spendGateToolRequest(ctx, "permission request", ev.Time)
}

func (s *Service) spendGateToolRequest(ctx context.Context, kind string, eventTime time.Time) (agenthooks.ToolPreDecision, error) {
	req := ingestPolicyRequestFrom(ctx)
	if req == nil || !spendGatedAdapter(req.payload.Source.Adapter) {
		return agenthooks.NoDecision(), nil
	}
	event := canonicalHookEvent(req.payload, req.authCtx, policyActor(ctx), eventTime)
	block := s.checkSpendGate(ctx, event)
	if block == nil {
		return agenthooks.NoDecision(), nil
	}
	auditReason := spendBlockReason(kind, block)
	userReason := s.appendCanonicalBlockURL(ctx, req.authCtx, policyActor(ctx), req.payload, auditReason, canonicalToolName(req.payload), "", auditReason)
	return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
}

// riskScanPromptGate scans a submitted prompt against the org's risk
// policies. A warn (challenge) holds the prompt for out-of-band
// acknowledgement when the ingest surface allows it: an acknowledged
// challenge lets the prompt through, an unacknowledged one denies with the
// ack link, and when no ack link can be produced the warn falls back to a
// plain block (fail-safe — a warn must never silently allow).
func (s *Service) riskScanPromptGate(ctx context.Context, ev *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
	var neutral agenthooks.PromptDecision
	req := ingestPolicyRequestFrom(ctx)
	if req == nil {
		return neutral, nil
	}
	event := canonicalHookEvent(req.payload, req.authCtx, policyActor(ctx), ev.Time)
	scan := s.scanUserPromptForEnforcement(ctx, hookevents.NewUserPromptSubmit(event, hookevents.UserPromptSubmitParams{
		Prompt: ev.Prompt,
	}))
	if scan == nil {
		return neutral, nil
	}
	if scan.Action == "warn" && authenticatedIngestOptions(ctx).AllowWarnAcknowledgement {
		if s.warnAcknowledged(ctx, event, scan, "") {
			return neutral, nil
		}
		if _, userReason, ok := s.warnDenyReason(ctx, event, scan, ""); ok {
			auditReason := fmt.Sprintf("Speakeasy challenged this prompt: matched policy %q (%s)", scan.PolicyName, scan.Description)
			return agenthooks.BlockPrompt(auditReason).WithSystemMessage(userReason), nil
		}
	}
	auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scan.PolicyName, scan.Description)
	return agenthooks.BlockPrompt(auditReason).
		WithSystemMessage(renderUserBlockReason(scan.UserMessage, auditReason)), nil
}

// riskScanToolPreGate scans a tool request against the org's risk policies,
// routing through the MCP-flavored event exactly as the inline evaluation
// did so saved CEL expressions evaluate identically.
func (s *Service) riskScanToolPreGate(ctx context.Context, ev *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return s.riskScanToolRequest(ctx, ev.Time)
}

// riskScanPermissionToolGate re-scans a pre-approval permission request as
// an MCP or plain tool request, after riskScanPermissionGate already scanned
// it with the permission flavor. The duplicate scan is deliberate: the old
// inline evaluation's permission branch did not return on a clean scan — it
// fell through into the MCP/plain branch and scanned the same request again
// with byte-identical scanner arguments. Dropping the second scan would be a
// semantic change (observably so for any non-deterministic scanner), so it
// is preserved exactly.
func (s *Service) riskScanPermissionToolGate(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return s.riskScanToolRequest(ctx, ev.Time)
}

// riskScanToolRequest is the shared MCP-or-plain risk scan behind
// riskScanToolPreGate and riskScanPermissionToolGate, with the "tool call"
// deny wording both inline branches used.
func (s *Service) riskScanToolRequest(ctx context.Context, eventTime time.Time) (agenthooks.ToolPreDecision, error) {
	req := ingestPolicyRequestFrom(ctx)
	if req == nil {
		return agenthooks.NoDecision(), nil
	}
	toolName := canonicalToolName(req.payload)
	toolInput := canonicalToolInput(req.payload)
	event := canonicalHookEvent(req.payload, req.authCtx, policyActor(ctx), eventTime)
	var scan *risk.ScanResult
	if canonicalIsMCPToolRequest(req.payload) {
		scan = s.scanMCPRequestForEnforcement(ctx, hookevents.NewBeforeMCPExecution(event, hookevents.BeforeMCPExecutionParams{
			ToolName:  toolName,
			ToolInput: toolInput,
		}))
	} else {
		scan = s.scanToolRequestForEnforcement(ctx, hookevents.NewBeforeToolUse(event, hookevents.BeforeToolUseParams{
			ToolName:  toolName,
			ToolInput: toolInput,
		}))
	}
	return s.riskScanToolDecision(ctx, req, event, scan, toolName, "tool call")
}

// riskScanPermissionGate scans a pre-approval permission request. It is the
// permission.request sibling of riskScanToolPreGate, with the
// permission-request scan and deny wording.
func (s *Service) riskScanPermissionGate(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	req := ingestPolicyRequestFrom(ctx)
	if req == nil {
		return agenthooks.NoDecision(), nil
	}
	toolName := canonicalToolName(req.payload)
	event := canonicalHookEvent(req.payload, req.authCtx, policyActor(ctx), ev.Time)
	scan := s.scanPermissionRequestForEnforcement(ctx, hookevents.NewPermissionRequest(event, hookevents.PermissionRequestParams{
		ToolName:       toolName,
		ToolInput:      canonicalToolInput(req.payload),
		PermissionType: canonicalPermissionType(req.payload),
	}))
	return s.riskScanToolDecision(ctx, req, event, scan, toolName, "permission request")
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
// model and the rendered user message (with the durable block-page URL
// minted for live deliveries) for the human.
func (s *Service) riskScanToolDecision(ctx context.Context, req *ingestPolicyRequest, event hookevents.Event, scan *risk.ScanResult, toolName string, blockedWhat string) (agenthooks.ToolPreDecision, error) {
	if scan == nil {
		return agenthooks.NoDecision(), nil
	}
	if scan.Action == "warn" {
		if s.warnAcknowledged(ctx, event, scan, toolName) {
			return agenthooks.NoDecision(), nil
		}
		if _, userReason, ok := s.warnDenyReason(ctx, event, scan, toolName); ok {
			auditReason := fmt.Sprintf("Speakeasy challenged this %s: matched policy %q (%s)", blockedWhat, scan.PolicyName, scan.Description)
			return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
		}
	}
	auditReason := fmt.Sprintf("Speakeasy blocked this %s: matched policy %q (%s)", blockedWhat, scan.PolicyName, scan.Description)
	userReason := renderUserBlockReason(scan.UserMessage, auditReason)
	userReason = s.appendCanonicalBlockURL(ctx, req.authCtx, policyActor(ctx), req.payload, auditReason, toolName, scan.PolicyID, userReason)
	return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
}

// shadowMCPToolPreGate / shadowMCPPermissionGate deny MCP tool calls that
// target non-Gram-hosted servers under a blocking shadow_mcp policy,
// attaching the bypass-request link. The shared gate guards itself with the
// ingest path's own MCP predicate (canonicalIsMCPToolRequest) — not the
// library's tool matcher, which parses Gemini-style mcp_server_tool names
// and server-only mcp__ prefixes the ingest path has never treated as MCP —
// so the gate fires for exactly the calls the inline evaluation gated and
// stays neutral (no policy lookup, no side effects) for everything else.
func (s *Service) shadowMCPToolPreGate(ctx context.Context, _ *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return s.shadowMCPGate(ctx)
}

func (s *Service) shadowMCPPermissionGate(ctx context.Context, _ *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return s.shadowMCPGate(ctx)
}

func (s *Service) shadowMCPGate(ctx context.Context) (agenthooks.ToolPreDecision, error) {
	req := ingestPolicyRequestFrom(ctx)
	if req == nil || !canonicalIsMCPToolRequest(req.payload) {
		return agenthooks.NoDecision(), nil
	}
	auditReason, userReason := s.evaluateCanonicalShadowMCP(ctx, req.authCtx, policyActor(ctx), req.payload, canonicalToolName(req.payload), canonicalToolInput(req.payload))
	if auditReason == "" {
		return agenthooks.NoDecision(), nil
	}
	return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
}
