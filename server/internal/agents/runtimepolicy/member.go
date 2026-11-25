package runtimepolicy

import (
	"context"
	"errors"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ResolveEligibleUser returns the principals of a user who is still an
// eligible member of the organization, or ok=false when they are not. A
// credential stops vouching for anything once the user it depends on leaves.
func ResolveEligibleUser(ctx context.Context, db accessrepo.DBTX, organizationID, userID string) ([]urn.Principal, bool, error) {
	principals, err := authz.ResolveUserPrincipals(ctx, db, organizationID, userID)
	if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("resolve user principals: %w", err)
	}
	user := urn.NewPrincipal(urn.PrincipalTypeUser, userID).String()
	for _, principal := range principals {
		if principal.String() == user {
			return principals, true, nil
		}
	}
	return nil, false, nil
}
