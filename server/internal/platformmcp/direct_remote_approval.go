package platformmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// DirectRemoteApprovalState reports whether existing Shadow MCP enforcement
// permits a registered user-supplied URL. It deliberately reflects the same
// policy and grant state used at runtime; a Platform MCP registration never
// creates an approval bypass.
type DirectRemoteApprovalState struct {
	EnforcementActive bool
	Approved          bool
}

// DirectRemoteApprovalTxChecker evaluates enforcement after attachment planning
// from the same distribution transaction snapshot as persistence. Policy reads
// are not locked, so a concurrent revoke remains race-narrowed rather than
// race-free.
type DirectRemoteApprovalTxChecker interface {
	CheckDirectRemoteApprovalTx(context.Context, riskrepo.DBTX, string, string, uuid.UUID, string) (DirectRemoteApprovalState, error)
}

// PostgresDirectRemoteApprovals reads enabled Shadow MCP policies and their
// URL-scoped grants. It is a narrow adapter over the existing enforcement data,
// not a parallel approval system.
type PostgresDirectRemoteApprovals struct{}

type directRemoteApprovalCandidate struct {
	policy             riskrepo.RiskPolicy
	allowAll           bool
	wholePolicyBypass  risk.PolicyBypassEvaluation
	serverPolicyBypass risk.PolicyBypassEvaluation
}

func NewPostgresDirectRemoteApprovals() *PostgresDirectRemoteApprovals {
	return &PostgresDirectRemoteApprovals{}
}

var _ DirectRemoteApprovalTxChecker = (*PostgresDirectRemoteApprovals)(nil)

func (c *PostgresDirectRemoteApprovals) CheckDirectRemoteApprovalTx(ctx context.Context, db riskrepo.DBTX, organizationID, userID string, projectID uuid.UUID, remoteURL string) (DirectRemoteApprovalState, error) {
	if c == nil || db == nil || organizationID == "" || userID == "" || projectID == uuid.Nil {
		return DirectRemoteApprovalState{}, ErrRegistrationUnavailable
	}
	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(remoteURL)
	if !ok {
		return DirectRemoteApprovalState{}, fmt.Errorf("canonicalize direct remote URL for approval consult: %w", ErrDirectRemoteRejected)
	}

	policies, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return DirectRemoteApprovalState{}, fmt.Errorf("list Shadow MCP policies for direct remote approval consult: %w", err)
	}

	principals, err := authz.ResolveUserPrincipals(ctx, db, organizationID, userID)
	if err != nil {
		return DirectRemoteApprovalState{}, fmt.Errorf("resolve direct remote approval principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, db, organizationID, principals)
	if err != nil {
		return DirectRemoteApprovalState{}, fmt.Errorf("load direct remote approval grants: %w", err)
	}
	candidates := make([]directRemoteApprovalCandidate, 0, len(policies))
	evaluations := make([]risk.PolicyBypassEvaluation, 0, len(policies)*2)
	for _, policy := range policies {
		if policy.Action != "block" || !authz.GrantsSatisfy(grants, authz.RiskPolicyEvaluateCheck(policy.ID.String())) {
			continue
		}
		wholeTarget := risk.WholePolicyBypassTarget()
		candidate := directRemoteApprovalCandidate{
			policy:   policy,
			allowAll: policy.ShadowMcpDisposition.Valid && policy.ShadowMcpDisposition.String == shadowmcp.DispositionAllowAll,
			serverPolicyBypass: risk.PolicyBypassEvaluation{
				OrganizationID: organizationID,
				UserID:         userID,
				PolicyID:       policy.ID.String(),
				Target:         nil,
			},
			wholePolicyBypass: risk.PolicyBypassEvaluation{
				OrganizationID: organizationID,
				UserID:         userID,
				PolicyID:       policy.ID.String(),
				Target:         &wholeTarget,
			},
		}
		evaluations = append(evaluations, candidate.wholePolicyBypass)
		if !candidate.allowAll {
			serverTarget := risk.ShadowMCPServerPolicyBypassTarget(inventoryURL.CanonicalURL, "", inventoryURL.CanonicalURL)
			candidate.serverPolicyBypass = risk.PolicyBypassEvaluation{
				OrganizationID: organizationID,
				UserID:         userID,
				PolicyID:       policy.ID.String(),
				Target:         &serverTarget,
			}
			evaluations = append(evaluations, candidate.serverPolicyBypass)
		}
		candidates = append(candidates, candidate)
	}
	decisions := risk.CanBypassBatchWithGrants(grants, evaluations)

	state := DirectRemoteApprovalState{EnforcementActive: false, Approved: true}
	for _, candidate := range candidates {
		// Match the runtime scanner's dimensionless policy audience check, then
		// evaluate bypasses through its canonicalizing evaluator. Keeping those
		// concerns separate avoids raw legacy bypass selectors disagreeing with
		// the runtime's URL-first policy-bypass semantics.
		if decisions[candidate.wholePolicyBypass] {
			continue
		}
		state.EnforcementActive = true

		// An allow-all policy blocks URLs project-wide; it has no user-scoped
		// bypass path. A block-all policy may be exempted by a canonical
		// URL-scoped grant, exactly as runtime does.
		if candidate.allowAll {
			blockGrants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
				OrganizationID: organizationID,
				Scope:          authz.ScopeRiskPolicyBlock,
				ResourceID:     candidate.policy.ID.String(),
			})
			if err != nil {
				return DirectRemoteApprovalState{}, fmt.Errorf("list direct remote block grants: %w", err)
			}
			for _, grant := range blockGrants {
				candidate, ok := shadowmcp.CanonicalizeInventoryURL(grant.Selector[authz.SelectorKeyServerURL])
				if ok && candidate.CanonicalURL == inventoryURL.CanonicalURL {
					state.Approved = false
					break
				}
			}
			continue
		}
		if !decisions[candidate.serverPolicyBypass] {
			state.Approved = false
		}
	}
	return state, nil
}
