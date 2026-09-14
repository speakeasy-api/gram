package authz

import (
	"context"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadUserGrants batches principal resolution and grant loading while keeping
// each user's policy separate. Like ResolveUserPrincipals, inactive and unknown
// users receive only user:all grants. The number of queries is independent of
// the number of users.
func LoadUserGrants(ctx context.Context, db accessrepo.DBTX, organizationID string, userIDs []string) (map[string][]Grant, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}
	result := make(map[string][]Grant, len(userIDs))
	uniqueIDs := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == urn.AllUsersPrincipalID {
			return nil, fmt.Errorf("%w: user id %q", ErrPrincipalInvalid, userID)
		}
		if _, ok := result[userID]; !ok {
			result[userID] = nil
			uniqueIDs = append(uniqueIDs, userID)
		}
	}
	if len(uniqueIDs) == 0 {
		return result, nil
	}

	rows, err := accessrepo.New(db).ListMemberPrincipalsByUsers(ctx, accessrepo.ListMemberPrincipalsByUsersParams{
		OrganizationID: organizationID,
		UserIds:        uniqueIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve user policy principals: %w", err)
	}
	principals := []urn.Principal{AllUsersPrincipal()}
	// A shared role can apply to several users, but another user's direct or
	// role grants must never become part of a user's runtime permission cap.
	principalUsers := map[string][]string{AllUsersPrincipal().String(): uniqueIDs}
	for _, row := range rows {
		principal, err := urn.ParsePrincipal(row.PrincipalUrn)
		if err != nil {
			return nil, fmt.Errorf("parse user policy principal: %w", err)
		}
		key := principal.String()
		if _, ok := principalUsers[key]; !ok {
			principals = append(principals, principal)
		}
		principalUsers[key] = append(principalUsers[key], row.UserID)
	}
	grants, err := LoadGrants(ctx, db, organizationID, principals)
	if err != nil {
		return nil, err
	}
	for _, grant := range grants {
		for _, userID := range principalUsers[grant.PrincipalUrn] {
			result[userID] = append(result[userID], grant)
		}
	}
	return result, nil
}
