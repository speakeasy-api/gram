package mcpapproval

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ReconcileStandingDecisionsForPolicy replays a project's recorded decisions
// onto one blocking shadow-MCP policy, inside the transaction that creates
// it. It implements the risk service's intake seam, so the policy write path
// never imports this package.
//
// Without this, ordering decides what an approval means: approve a server
// while no blocking policy exists and nothing has a grant to write, then
// create the policy and the server is blocked while its review still reads
// approved. The decision record stores its blast radius precisely so a later
// policy can honor it — this is where it does.
//
// The single-blocking-policy invariant makes the replay total: creating a
// blocking policy means no other blocking policy existed, so every standing
// decision's grant state on this policy is derived here from nothing.
//
// One combination aborts the creation rather than being approximated,
// mirroring decision-time enforcement: an allow_all policy cannot express an
// approval narrower than everyone. Widening it silently would make the
// recorded decision lie about who was given access, so the policy creation
// fails and names the servers; the admin re-decides them or picks
// block-by-default.
func (s *Service) ReconcileStandingDecisionsForPolicy(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, policyID uuid.UUID) error {
	policy, err := riskrepo.New(tx).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	if err != nil {
		return fmt.Errorf("load policy for standing-decision backfill: %w", err)
	}
	if !policy.Enabled || policy.Action != "block" || !slices.Contains(policy.Sources, shadowmcp.SourceShadowMCP) {
		// Not a blocking shadow-MCP policy; there is no grant state to derive.
		return nil
	}

	standing, err := repo.New(tx).ListStandingServerDecisionsForProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list standing decisions for backfill: %w", err)
	}

	// The inexpressible-radius check runs before any write, so a rejected
	// creation leaves nothing half-applied even within the transaction.
	if policyDisposition(policy) == shadowmcp.DispositionAllowAll {
		var narrow []string
		for _, row := range standing {
			if row.Decision != decisionApproved {
				continue
			}
			principals, err := parseGrantedPrincipals(row.GrantedPrincipalUrns)
			if err != nil {
				return err
			}
			if !principalsAreEveryone(principals) {
				narrow = append(narrow, row.TargetRaw)
			}
		}
		if len(narrow) > 0 {
			return oops.E(oops.CodeBadRequest, nil,
				"This project has standing approvals scoped to specific people (%s), and an allow-by-default policy can only clear blocks for everyone. Re-decide those servers for everyone, or create the policy as block-by-default.",
				strings.Join(narrow, ", "))
		}
	}

	for _, row := range standing {
		principals, err := parseGrantedPrincipals(row.GrantedPrincipalUrns)
		if err != nil {
			return err
		}
		if err := applyDecisionToPolicy(ctx, tx, organizationID, policy, row.TargetKey, row.Decision == decisionApproved, principals); err != nil {
			return fmt.Errorf("backfill decision for %s: %w", row.TargetKey, err)
		}
	}

	return nil
}

// parseGrantedPrincipals reads a decision's stored blast radius. The URNs
// were validated at intake, so a row that no longer parses is corrupt data
// worth failing on, not something to skip past.
func parseGrantedPrincipals(raw []string) ([]urn.Principal, error) {
	principals := make([]urn.Principal, 0, len(raw))
	for _, value := range raw {
		principal, err := urn.ParsePrincipal(value)
		if err != nil {
			return nil, fmt.Errorf("parse stored granted principal %q: %w", value, err)
		}
		principals = append(principals, principal)
	}
	return principals, nil
}
