package agentmanagement

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ListDelegableGrants exposes only safe issuance candidates, never raw policy.
func (s *Service) ListDelegableGrants(ctx context.Context, payload *gen.ListDelegableGrantsPayload) ([]*gen.AgentPolicyGrantForm, error) {
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
		evaluation, _ := feature.EvaluateFlag(ctx, s.features, feature.FlagAgentIdentityCredentials, human.Auth.ActiveOrganizationID, feature.OrgProjectGroups(human.Auth.OrganizationSlug, ""))
		if evaluation != feature.EvaluationEnabled {
			return oops.C(oops.CodeNotFound)
		}
		if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
			return oops.C(oops.CodeForbidden)
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
		grants, err := runtimepolicy.DelegableGrants(agentPolicy, ownerPolicy, human.grants)
		if err != nil {
			return fmt.Errorf("derive delegable grants: %w", err)
		}
		for _, grant := range grants {
			result = append(result, &gen.AgentPolicyGrantForm{Scope: string(grant.Scope), Effect: "allow", Selector: policySelectorView(grant.Selector)})
		}
		return nil
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, "list delegable grants")
	}
	return result, nil
}
