package mcpapproval

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// reconcileDecisionGrants makes a decision enforce: it writes the same
// authz grants the shadow-MCP allow/block controls manage, so the policy
// evaluator honors an approval without knowing decisions exist. This is what
// keeps the workflow a single flow — the decision is the only write path to
// enforcement, and the per-server grant state is derived from it.
//
// The grant direction depends on each blocking policy's disposition, exactly
// mirroring the inventory controls:
//
//   - block_all (and legacy, which defaults to block_all): servers are blocked
//     unless excepted. Approve replaces the server's risk_policy:bypass
//     audience with the decision's principals; deny revokes it.
//   - allow_all: servers are allowed unless block-ruled. Approve revokes the
//     server's risk_policy:block rule; deny writes one for everyone.
//
// Only server_url targets are enforceable — grants key on the canonical URL,
// and an stdio command has none. Those decisions still record; the caller is
// responsible for surfacing that they do not enforce.
//
// One combination is rejected rather than approximated: an approval narrower
// than everyone when only allow_all policies govern. The evaluator never
// consults bypass grants under an allow_all block rule — a server there is
// block-ruled for everyone or allowed for everyone — so enforcing a named
// blast radius is impossible, and silently widening to everyone would make
// the recorded decision lie about who was given access. With a block_all
// policy also present the narrow radius composes correctly (the block_all
// bypass audience gates who actually passes), so the rejection only fires
// when nothing can express the radius.
func reconcileDecisionGrants(
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	projectID uuid.UUID,
	canonicalURL string,
	approved bool,
	principals []urn.Principal,
) error {
	rows, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list shadow mcp policies for decision: %w", err)
	}

	blocking := make([]riskrepo.RiskPolicy, 0, len(rows))
	hasBlockAll := false
	hasAllowAll := false
	for _, policy := range rows {
		if policy.Action != "block" {
			continue
		}
		blocking = append(blocking, policy)
		if policyDisposition(policy) == shadowmcp.DispositionAllowAll {
			hasAllowAll = true
		} else {
			hasBlockAll = true
		}
	}

	if approved && !principalsAreEveryone(principals) && hasAllowAll && !hasBlockAll {
		return oops.E(oops.CodeBadRequest, nil, "This project's policy allows servers by default, so an approval can only clear the block for everyone. Approve without naming principals, or switch the policy to block-by-default for per-person approvals.")
	}

	for _, policy := range blocking {
		policyID := policy.ID.String()

		switch policyDisposition(policy) {
		case shadowmcp.DispositionAllowAll:
			if approved {
				// The server is allowed by default; approval clears any block
				// rule standing in the way. Blast radius does not apply — an
				// allow_all policy has no per-principal allow concept.
				if err := policybypass.RevokePolicyURL(ctx, db, organizationID, authz.ScopeRiskPolicyBlock, policyID, canonicalURL); err != nil {
					return fmt.Errorf("revoke block rule on approval: %w", err)
				}
				continue
			}
			if err := policybypass.ReplacePolicyURLAudience(ctx, db, organizationID, authz.ScopeRiskPolicyBlock, policyID, canonicalURL, []urn.Principal{authz.AllUsersPrincipal()}); err != nil {
				return fmt.Errorf("write block rule on denial: %w", err)
			}
		default:
			if approved {
				if err := policybypass.ReplacePolicyURLAudience(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL, principals); err != nil {
					return fmt.Errorf("replace bypass audience on approval: %w", err)
				}
				continue
			}
			// A denial leaves the policy's default standing: blocked, with no
			// exceptions for this server.
			if err := policybypass.RevokePolicyURL(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL); err != nil {
				return fmt.Errorf("revoke bypass audience on denial: %w", err)
			}
		}
	}

	return nil
}

// policyDisposition resolves a blocking policy's shadow-MCP disposition,
// defaulting legacy rows to block_all the way the policy setup flow does.
func policyDisposition(policy riskrepo.RiskPolicy) string {
	if policy.ShadowMcpDisposition.Valid && policy.ShadowMcpDisposition.String != "" {
		return policy.ShadowMcpDisposition.String
	}
	return shadowmcp.DispositionBlockAll
}

// principalsAreEveryone reports whether the blast radius is the whole
// organization — the one radius an allow_all policy can express.
func principalsAreEveryone(principals []urn.Principal) bool {
	allUsers := authz.AllUsersPrincipal()
	for _, principal := range principals {
		if principal != allUsers {
			return false
		}
	}
	return len(principals) > 0
}
