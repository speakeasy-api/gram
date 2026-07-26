package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/hooks/policies"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Enforcer owns the hook enforcement dependencies and primitives: the risk
// scans, the shadow-MCP evaluation, and block-page URL minting. It exists
// apart from Service so cmd can construct it first and hand the same value
// to both policies.NewRunner (as the runner's policies.Deps) and NewService.
// Service embeds it, so the legacy per-provider paths keep reading the
// promoted fields and methods (s.riskScanner, s.scanToolRequestForEnforcement,
// ...) exactly as before — one copy of each dependency, shared with the
// policy runner. Tests that swap a field after construction
// (ti.service.riskScanner = ...) mutate that shared state, so the runner's
// stages observe the swap on the next event.
type Enforcer struct {
	logger          *slog.Logger
	repo            *repo.Queries
	cache           cache.Cache
	riskScanner     risk.RiskScanner
	policyBypass    *risk.PolicyBypassEvaluator
	spendGate       *spendrules.Gate
	shadowMCPClient *shadowmcp.Client
	siteURL         *url.URL
	jwtSecret       string
}

// NewEnforcer builds the enforcement component from the dependencies the
// gating paths read. The logger is tagged with the hooks component, matching
// the Service logger it is promoted into.
func NewEnforcer(
	logger *slog.Logger,
	db *pgxpool.Pool,
	cacheAdapter cache.Cache,
	riskScanner risk.RiskScanner,
	policyBypass *risk.PolicyBypassEvaluator,
	spendGate *spendrules.Gate,
	shadowMCPClient *shadowmcp.Client,
	siteURL *url.URL,
	jwtSecret string,
) *Enforcer {
	return &Enforcer{
		logger:          logger.With(attr.SlogComponent("hooks")),
		repo:            repo.New(db),
		cache:           cacheAdapter,
		riskScanner:     riskScanner,
		policyBypass:    policyBypass,
		spendGate:       spendGate,
		shadowMCPClient: shadowMCPClient,
		siteURL:         siteURL,
		jwtSecret:       jwtSecret,
	}
}

// The methods below are the policies.Deps facade: Request-shaped adapters
// over the enforcement primitives, called by the policy stages registered in
// policies.NewRunner. They project the canonical payload exactly as the old
// inline stages did (canonicalHookEvent with the middleware-resolved actor
// and the stage's event time), so a policy written against the interfaces
// sees byte-identical scanner arguments.

var _ policies.Deps = (*Enforcer)(nil)

// CheckSpend consults the spend-rule circuit for the actor and renders the
// deny reason. It owns the whole spend decision the stages act on: the
// adapter gating (only claude/codex/cursor have a per-provider enforcement
// surface; matching is case-insensitive so a case variant cannot dodge the
// gate), the circuit lookup (fail-open on infrastructure errors), and the
// reason wording. blocked=false covers every non-deny outcome.
func (e *Enforcer) CheckSpend(ctx context.Context, req *policies.Request, actor policies.Actor, kind string, at time.Time) (string, bool) {
	if !spendGatedAdapter(req.Payload.Source.Adapter) {
		return "", false
	}
	block := e.checkSpendGate(ctx, canonicalHookEvent(req.Payload, req.AuthCtx, actor, at))
	if block == nil {
		return "", false
	}
	return spendBlockReason(kind, block), true
}

// WarnAcknowledged reports whether the user has a live acknowledgement for a
// warn (challenge) match, so the retried call should be allowed. Only
// meaningful when scan.Action == "warn".
func (e *Enforcer) WarnAcknowledged(ctx context.Context, req *policies.Request, actor policies.Actor, scan *risk.ScanResult, toolName string, at time.Time) bool {
	return e.warnAcknowledged(ctx, canonicalHookEvent(req.Payload, req.AuthCtx, actor, at), scan, toolName)
}

// WarnDenyReason records the challenge and returns the two framings of the
// deny (model-facing without the ack link, human-facing with it). ok=false
// means an ack link could not be produced — the caller MUST fall back to a
// plain block (fail-safe): a warn must never silently allow.
func (e *Enforcer) WarnDenyReason(ctx context.Context, req *policies.Request, actor policies.Actor, scan *risk.ScanResult, toolName string, at time.Time) (string, string, bool) {
	return e.warnDenyReason(ctx, canonicalHookEvent(req.Payload, req.AuthCtx, actor, at), scan, toolName)
}

// ScanPrompt runs the prompt-flavored enforcement risk scan.
func (e *Enforcer) ScanPrompt(ctx context.Context, req *policies.Request, actor policies.Actor, prompt string, at time.Time) *risk.ScanResult {
	return e.scanUserPromptForEnforcement(ctx, hookevents.NewUserPromptSubmit(
		canonicalHookEvent(req.Payload, req.AuthCtx, actor, at),
		hookevents.UserPromptSubmitParams{Prompt: prompt},
	))
}

// ScanMCPToolRequest runs the MCP-flavored enforcement risk scan over a tool
// request.
func (e *Enforcer) ScanMCPToolRequest(ctx context.Context, req *policies.Request, actor policies.Actor, at time.Time) *risk.ScanResult {
	return e.scanMCPRequestForEnforcement(ctx, hookevents.NewBeforeMCPExecution(
		canonicalHookEvent(req.Payload, req.AuthCtx, actor, at),
		hookevents.BeforeMCPExecutionParams{
			ToolName:  req.ToolName,
			ToolInput: req.ToolInput,
		},
	))
}

// ScanToolRequest runs the plain tool-flavored enforcement risk scan over a
// tool request.
func (e *Enforcer) ScanToolRequest(ctx context.Context, req *policies.Request, actor policies.Actor, at time.Time) *risk.ScanResult {
	return e.scanToolRequestForEnforcement(ctx, hookevents.NewBeforeToolUse(
		canonicalHookEvent(req.Payload, req.AuthCtx, actor, at),
		hookevents.BeforeToolUseParams{
			ToolName:  req.ToolName,
			ToolInput: req.ToolInput,
		},
	))
}

// ScanPermissionRequest runs the permission-flavored enforcement risk scan
// over a pre-approval permission request.
func (e *Enforcer) ScanPermissionRequest(ctx context.Context, req *policies.Request, actor policies.Actor, at time.Time) *risk.ScanResult {
	return e.scanPermissionRequestForEnforcement(ctx, hookevents.NewPermissionRequest(
		canonicalHookEvent(req.Payload, req.AuthCtx, actor, at),
		hookevents.PermissionRequestParams{
			ToolName:       req.ToolName,
			ToolInput:      req.ToolInput,
			PermissionType: canonicalPermissionType(req.Payload),
		},
	))
}

// AppendBlockPageURL mints the durable block row for a policy-denied tool
// call and attaches its URL to the agent-facing reason, matching the legacy
// per-provider handlers. Retried deliveries keep the deny but must not mint
// a second row.
func (e *Enforcer) AppendBlockPageURL(ctx context.Context, req *policies.Request, actor policies.Actor, auditReason, toolName, policyID, userReason string) string {
	if e.isHookDuplicate(ctx) {
		return userReason
	}
	bURL := e.recordToolCallBlockAsync(ctx, toolCallBlockParams{
		Provider:       strings.TrimSpace(req.Payload.Source.Adapter),
		OrganizationID: req.AuthCtx.ActiveOrganizationID,
		ProjectID:      *req.AuthCtx.ProjectID,
		Reason:         auditReason,
		ToolName:       toolName,
		UserID:         actor.UserID,
		RiskPolicyID:   conv.StringToNullUUID(policyID),
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock(canonicalSessionID(req.Payload)),
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if bURL == "" {
		return userReason
	}
	return appendBlockURL(userReason, bURL)
}

// EvaluateShadowMCP runs the shadow-MCP enforcement primitive over an MCP
// tool request: the blocking shadow_mcp policy lookup, the Gram-hosted
// check, the bypass-grant check, and — on a deny — the access-request link
// and block row. Empty reasons mean the call is not denied.
func (e *Enforcer) EvaluateShadowMCP(ctx context.Context, req *policies.Request, actor policies.Actor, rawToolName string, toolInput any) (string, string) {
	authCtx := req.AuthCtx
	policy := e.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), actor.UserID)
	if policy == nil {
		return "", ""
	}

	toolName := toolref.MCPFunctionOf(rawToolName)
	evidence := canonicalShadowMCPEvidence(req.Payload, rawToolName)
	if detail, denied := e.enforceShadowMCPToolAccess(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), actor.UserID, policy, toolName, evidence); denied {
		auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		userReason := e.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       authCtx.ProjectID.String(),
			RequesterUserID: actor.UserID,
			UserMessage:     policy.UserMessage,
			AuditReason:     auditReason,
			Evidence:        evidence,
			ToolName:        toolName,
			ToolInput:       toolInput,
			RiskPolicyID:    policy.ID,
		})
		// Retried deliveries still get the deny decision, but must not mint
		// another block row (and a second block URL) for the same call.
		if !e.isHookDuplicate(ctx) {
			if bURL := e.recordToolCallBlockAsync(ctx, toolCallBlockParams{
				Provider:       strings.TrimSpace(req.Payload.Source.Adapter),
				OrganizationID: authCtx.ActiveOrganizationID,
				ProjectID:      *authCtx.ProjectID,
				Reason:         auditReason,
				ToolName:       toolName,
				UserID:         actor.UserID,
				RiskPolicyID:   conv.StringToNullUUID(policy.ID),
				RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ChatID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			}); bURL != "" {
				userReason = appendBlockURL(userReason, bURL)
			}
		}
		return auditReason, userReason
	}
	return "", ""
}
