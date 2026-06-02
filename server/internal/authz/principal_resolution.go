package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrPrincipalNotFound reports that a Gram user could not be resolved to an
// active in-organization principal.
var ErrPrincipalNotFound = errors.New("principal not found")

// ResolveUserPrincipals resolves user:<id> plus assigned role principals
// only when the Gram user is an active member of the organization. Missing or
// cross-org users return ErrPrincipalNotFound.
func ResolveUserPrincipals(ctx context.Context, db repo.DBTX, organizationID string, userID string) ([]urn.Principal, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}
	if userID == "" {
		return nil, ErrPrincipalNotFound
	}

	isMember, err := orgrepo.New(db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve organization user principal: %w", err)
	}
	if !isMember {
		return nil, ErrPrincipalNotFound
	}

	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, userID)}
	seen := map[string]struct{}{principals[0].String(): {}}

	q := repo.New(db)
	roleRows, err := q.ListMemberRolePrincipalsByUser(ctx, repo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve role principals: %w", err)
	}

	for _, role := range roleRows {
		rolePrincipals, err := RolePrincipals(role.RoleSlug, role.PrincipalUrn)
		if err != nil {
			return nil, err
		}
		for _, principal := range rolePrincipals {
			key := principal.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			principals = append(principals, principal)
		}
	}

	return principals, nil
}
