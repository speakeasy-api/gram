package agentmanagement

import (
	"context"
	"fmt"
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// ListEligibleOwners reveals only active members the caller may provision for,
// without granting access to the organization's general member directory.
func (s *Service) ListEligibleOwners(ctx context.Context, _ *gen.ListEligibleOwnersPayload) ([]*gen.AgentEligibleOwner, error) {
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list eligible agent owners")
	}
	if !slices.ContainsFunc(human.grants, func(grant authz.Grant) bool {
		return grant.Scope == authz.ScopeAgentCreate || grant.Scope == authz.ScopeRoot
	}) {
		return nil, oops.C(oops.CodeForbidden)
	}
	rows, err := orgrepo.New(s.db).ListOrganizationUsers(ctx, human.Auth.ActiveOrganizationID)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list eligible agent owners")
	}
	owners := make([]*gen.AgentEligibleOwner, 0)
	for _, row := range rows {
		for _, check := range authz.AgentCreationChecks(human.Auth.UserID, row.UserID.String) {
			allowed, err := authz.GrantsAuthorize(human.grants, check)
			if err != nil {
				return nil, s.serviceError(ctx, fmt.Errorf("evaluate eligible agent owner: %w", err), "list eligible agent owners")
			}
			if allowed {
				owners = append(owners, &gen.AgentEligibleOwner{UserID: row.UserID.String, DisplayName: row.UserDisplayName})
				break
			}
		}
	}
	return owners, nil
}
