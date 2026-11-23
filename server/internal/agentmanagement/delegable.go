package agentmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ListDelegableGrants exposes only safe issuance candidates, never raw policy.
func (s *Service) ListDelegableGrants(ctx context.Context, payload *gen.ListDelegableGrantsPayload) ([]*gen.AgentPolicyGrantForm, error) {
	// Validate the ordinary authenticated session before any transaction,
	// owner lookup, membership lock, or grant loading.
	if _, err := ordinaryHumanAuth(ctx); err != nil {
		return nil, err
	}
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	result := make([]*gen.AgentPolicyGrantForm, 0)
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, agent, err := s.authorizer.RequireAgentOwnerForUpdate(ctx, tx, agentID, OwnedAgentAuthorize)
		if err != nil {
			return fmt.Errorf("authorize delegable grant discovery: %w", err)
		}
		if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
			return oops.C(oops.CodeForbidden)
		}
		toolsetIDs := payload.ToolsetIds
		if payload.ToolsetID != nil {
			toolsetIDs = append([]string{*payload.ToolsetID}, toolsetIDs...)
		}
		// One nil constraint means unscoped discovery. Each resource otherwise
		// gets its own constraint, evaluated under this single agent lock so
		// batch discovery does not queue per-resource lock waits.
		constraints := []authz.Selector{nil}
		if len(toolsetIDs) > 0 {
			constraints = make([]authz.Selector, 0, len(toolsetIDs))
		}
		for _, toolsetID := range toolsetIDs {
			resourceID, err := uuid.Parse(toolsetID)
			if err != nil {
				return oops.C(oops.CodeBadRequest)
			}
			// The wire field retains its legacy name, but inventory normalizes
			// toolset-backed servers to their toolset ID and remote servers to
			// their MCP server ID. Resolve both through the existing RBAC lookup.
			projectID, err := accessrepo.New(tx).FindMCPResourceProject(ctx, accessrepo.FindMCPResourceProjectParams{
				ResourceID: resourceID, OrganizationID: human.Auth.ActiveOrganizationID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				// Inventory can outlive a resource (including a server's backing
				// toolset). An unavailable batch member contributes no grants;
				// it must not prevent discovery for the remaining live resources.
				// Preserve the legacy single-resource not-found contract.
				if payload.ToolsetID != nil && toolsetID == *payload.ToolsetID {
					return oops.C(oops.CodeNotFound)
				}
				continue
			}
			if err != nil {
				return fmt.Errorf("load delegable MCP resource: %w", err)
			}
			constraint := authz.NewSelector(authz.ScopeMCPConnect, resourceID.String())
			constraint[authz.SelectorKeyProjectID] = projectID.String()
			constraints = append(constraints, constraint)
		}
		agentPolicy, err := runtimepolicy.LoadAgentPolicy(ctx, tx, human.Auth.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()))
		if err != nil {
			return fmt.Errorf("load delegable agent policy: %w", err)
		}
		ownerPrincipals, err := authz.ResolveUserPrincipals(ctx, tx, human.Auth.ActiveOrganizationID, agent.OwnerUserID)
		if err != nil {
			return fmt.Errorf("resolve delegable owner principals: %w", err)
		}
		ownerPolicy, err := authz.LoadGrants(ctx, tx, human.Auth.ActiveOrganizationID, ownerPrincipals)
		if err != nil {
			return fmt.Errorf("load delegable owner policy: %w", err)
		}
		// Carrying keeps a wildcard that one blocked server overlaps, such as
		// "every server in the project" for an owner blocked from one of them.
		// A key that stores the wildcard reaches servers added after it is
		// issued; one that stores today's servers never does.
		discover := runtimepolicy.DelegableGrantsFor
		if payload.CarryExclusions {
			discover = runtimepolicy.DelegableGrantsWithExclusionsFor
		}
		seen := make(map[string]struct{})
		for _, constraint := range constraints {
			grants, err := discover(human.Auth.UserID, agentPolicy, ownerPolicy, human.grants, constraint)
			if err != nil {
				return fmt.Errorf("derive delegable grants: %w", err)
			}
			for _, grant := range grants {
				if !runtimepolicy.IsIssuableScope(grant.Scope) {
					continue
				}
				encoded, err := json.Marshal(grant.Selector)
				if err != nil {
					return fmt.Errorf("encode delegable selector: %w", err)
				}
				key := string(grant.Scope) + "\x00" + string(encoded)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, &gen.AgentPolicyGrantForm{Scope: string(grant.Scope), Effect: "allow", Selector: policySelectorView(grant.Selector)})
				// Each call is capped, but the union across resources must be too.
				if len(result) > runtimepolicy.MaxDelegableGrantCandidates {
					return runtimepolicy.ErrTooManyDelegableGrantCandidates
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, "list delegable grants")
	}
	return result, nil
}
