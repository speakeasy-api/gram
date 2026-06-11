package hooks

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpname"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func (s *Service) enforceShadowMCPToolAccess(
	ctx context.Context,
	organizationID string,
	projectID string,
	userID string,
	policyID string,
	toolInput any,
	toolName string,
	evidence shadowmcp.AccessEvidence,
) (string, bool) {
	detail, denied := s.shadowMCPClient.ValidateToolsetCall(ctx, toolInput, toolName, organizationID)
	if !denied {
		return "", false
	}
	if target, allowed := s.canBypassPolicy(ctx, organizationID, userID, policyID, evidence, toolName); allowed {
		s.logger.InfoContext(ctx, "shadow-mcp call allowed via risk policy bypass grant",
			attr.SlogEvent("shadow_mcp_policy_bypass_allow"),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policyID),
			attr.SlogValueAny(map[string]any{
				"target_kind": target.Kind,
				"target_key":  target.Key,
				"tool_name":   toolName,
			}),
		)
		return "", false
	}
	return detail, true
}

func (s *Service) canBypassPolicy(
	ctx context.Context,
	organizationID string,
	userID string,
	policyID string,
	evidence shadowmcp.AccessEvidence,
	toolName string,
) (*risk.PolicyBypassTarget, bool) {
	target := risk.ShadowMCPPolicyBypassTarget(evidence, toolName)
	if target == nil {
		return nil, false
	}
	allowed := s.policyBypass.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: organizationID,
		UserID:         userID,
		PolicyID:       policyID,
		Target:         target,
	})
	if !allowed {
		return nil, false
	}
	return target, true
}

// codexShadowMCPEvidence builds the access evidence for a Codex shadow-MCP
// tool call. The tool-name prefix gives the server identity; when the
// SessionStart inventory snapshot resolves that prefix to a configured
// server, its URL is attached so bypass grants and access requests can be
// scoped to the server URL rather than just the name.
func (s *Service) codexShadowMCPEvidence(ctx context.Context, payload *gen.CodexPayload) shadowmcp.AccessEvidence {
	evidence := shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: mcpServerIdentityFromToolName(conv.PtrValOr(payload.ToolName, "")),
	}
	sessionID := conv.PtrValOr(payload.SessionID, "")
	if evidence.ServerIdentity == "" || sessionID == "" {
		return evidence
	}
	entries, err := s.getCachedMCPList(ctx, sessionID)
	if err != nil {
		return evidence
	}
	if matched := matchCachedMCPEntry(entries, evidence.ServerIdentity); matched != nil && matched.URL != "" {
		evidence.FullURL = matched.URL
	}
	return evidence
}

func cursorShadowMCPEvidence(payload *gen.CursorPayload) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        conv.PtrValOr(payload.URL, ""),
		URLHost:        "",
		ServerIdentity: cursorMCPToolSource(payload),
	}
}

func claudeShadowMCPEvidence(rawToolName string) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: mcpServerIdentityFromToolName(rawToolName),
	}
}

// mcpServerIdentityFromToolName extracts the MCP server identity from a Codex
// or Claude tool name. Both hosts emit the "mcp__<server>__<tool>" form, so the
// shared parser's Cursor "MCP:" branch never fires here.
func mcpServerIdentityFromToolName(rawName string) string {
	return mcpname.MCPServerOf(rawName)
}
