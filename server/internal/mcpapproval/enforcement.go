package mcpapproval

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	// ErrNarrowApprovalUnderAllowAll marks an approval narrower than everyone
	// when only allow-by-default policies govern, which cannot express it.
	ErrNarrowApprovalUnderAllowAll = errors.New("approval narrower than everyone under an allow-by-default policy")

	// ErrNarrowDenialUnderEveryoneBlockAll marks a denial narrower than
	// everyone under a block-by-default policy that covers everyone, where a
	// denial blocks everyone.
	ErrNarrowDenialUnderEveryoneBlockAll = errors.New("denial narrower than everyone under a block-by-default policy for everyone")
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
//     server's risk_policy:block rule; deny writes one for the people or
//     roles the denial names, or for everyone when it names none.
//
// Only server_url targets are enforceable — grants key on the canonical URL,
// and an stdio command has none. Platform callers reject stdio targets; the
// dashboard may still record those decisions without calling this function.
//
// Two combinations are rejected rather than approximated. The first is an
// approval narrower than everyone when only allow_all policies govern. An
// allow_all policy has no per-principal allow: an approval only clears its
// block rule, so enforcing a named blast radius is impossible, and silently
// widening to everyone would make the recorded decision lie about who was
// given access. Denials under allow_all can name people or roles, since a
// block rule can. With a block_all policy also present the narrow approval
// composes correctly (the block_all bypass audience gates who actually
// passes), so that rejection only fires when nothing can express the radius.
// The second is a denial narrower than everyone under a block_all policy
// that covers everyone: the denial revokes every exception, which blocks the
// server for everyone, so a record naming only some people would understate
// who is blocked.
func reconcileDecisionGrants(
	ctx context.Context,
	db pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	canonicalURL string,
	approved bool,
	principals []urn.Principal,
) error {
	// DecideInTransaction already holds the project enforcement lock before its
	// request row lock. Keeping lock acquisition at the command boundary gives
	// every standing-decision writer one global order.
	rows, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list shadow mcp policies for decision: %w", err)
	}

	blocking := make([]riskrepo.RiskPolicy, 0, len(rows))
	hasBlockAll := false
	hasEveryoneBlockAll := false
	hasAllowAll := false
	for _, policy := range rows {
		if policy.Action != "block" {
			continue
		}
		blocking = append(blocking, policy)
		if policyDisposition(policy) == shadowmcp.DispositionAllowAll {
			hasAllowAll = true
			continue
		}
		hasBlockAll = true
		if policy.AudienceType == policyAudienceEveryone {
			hasEveryoneBlockAll = true
		}
	}

	if approved && !principalsAreEveryone(principals) && hasAllowAll && !hasBlockAll {
		return oops.E(oops.CodeBadRequest, ErrNarrowApprovalUnderAllowAll, "This project's policy allows servers by default, so an approval can only clear the block for everyone. Approve without naming principals, or switch the policy to block-by-default for per-person approvals.")
	}
	if !approved && len(principals) > 0 && !principalsAreEveryone(principals) && hasEveryoneBlockAll {
		return oops.E(oops.CodeBadRequest, ErrNarrowDenialUnderEveryoneBlockAll, "This project blocks servers by default for everyone, so a denial blocks the server for everyone. Deny without naming principals.")
	}

	for _, policy := range blocking {
		if err := applyDecisionToPolicy(ctx, db, organizationID, policy, canonicalURL, approved, principals); err != nil {
			return err
		}
	}

	return nil
}

// applyDecisionToPolicy writes one decision's grant state onto one blocking
// policy. Shared by decision-time enforcement (every blocking policy, one
// decision) and the policy-creation backfill (one new policy, every standing
// decision), so the two can never disagree on what a decision means.
//
// Every path revokes by grant variant, not by the exact URL-only selector:
// legacy access-request approvals persisted {server_url, server_identity}
// selectors that evaluate URL-only at runtime, and an exact-selector
// revoke would leave those grants standing — a legacy-approved user would
// survive a recorded deny.
func applyDecisionToPolicy(
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	policy riskrepo.RiskPolicy,
	canonicalURL string,
	approved bool,
	principals []urn.Principal,
) error {
	policyID := policy.ID.String()

	switch policyDisposition(policy) {
	case shadowmcp.DispositionAllowAll:
		if approved {
			// The server is allowed by default; approval clears any block
			// rule standing in the way. Blast radius does not apply — an
			// allow_all policy has no per-principal allow concept.
			if err := policybypass.RevokePolicyURLGrantVariants(ctx, db, organizationID, authz.ScopeRiskPolicyBlock, policyID, canonicalURL); err != nil {
				return fmt.Errorf("revoke block rule on approval: %w", err)
			}
			return nil
		}
		if err := policybypass.RevokePolicyURLGrantVariants(ctx, db, organizationID, authz.ScopeRiskPolicyBlock, policyID, canonicalURL); err != nil {
			return fmt.Errorf("revoke block rule variants on denial: %w", err)
		}
		// A denial narrowed to people or roles blocks only them; one with no
		// principals blocks everyone.
		blocked := principals
		if len(blocked) == 0 {
			blocked = []urn.Principal{authz.AllUsersPrincipal()}
		}
		if err := policybypass.ReplacePolicyURLAudience(ctx, db, organizationID, authz.ScopeRiskPolicyBlock, policyID, canonicalURL, blocked); err != nil {
			return fmt.Errorf("write block rule on denial: %w", err)
		}
	default:
		if approved {
			if err := policybypass.RevokePolicyURLGrantVariants(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL); err != nil {
				return fmt.Errorf("revoke bypass variants on approval: %w", err)
			}
			if err := policybypass.ReplacePolicyURLAudience(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL, principals); err != nil {
				return fmt.Errorf("replace bypass audience on approval: %w", err)
			}
			return nil
		}
		// A denial leaves the policy's default standing: blocked, with no
		// exceptions for this server.
		if err := policybypass.RevokePolicyURLGrantVariants(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL); err != nil {
			return fmt.Errorf("revoke bypass audience on denial: %w", err)
		}
	}

	return nil
}

// policyAudienceEveryone is a risk policy that applies to everyone in the
// project rather than to chosen people or roles.
const policyAudienceEveryone = "everyone"

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
