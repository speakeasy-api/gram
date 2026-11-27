package authz

import (
	"context"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadGrants loads and normalizes grants for the given organization and
// principals. Stored grants come from principal_grants; standard roles
// (standard_admin, standard_member) contribute their canonical grants from
// registry.Definitions alongside any stored non-role grants (such as risk
// policies). Other roles synthesize no defaults, so a role edit that removes a
// grant takes effect immediately.
func LoadGrants(ctx context.Context, db accessrepo.DBTX, organizationID string, principals []urn.Principal) ([]Grant, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	principalURNs, err := principalURNStrings(principals)
	if err != nil {
		return nil, err
	}

	grantRows, err := loadStoredGrants(ctx, db, organizationID, principalURNs)
	if err != nil {
		return nil, err
	}

	standardGrants, err := LoadStandardRoleGrants(ctx, db, principalURNs)
	if err != nil {
		return nil, err
	}
	grantRows = append(grantRows, standardGrants...)

	return grantRows, nil
}

func loadStoredGrants(ctx context.Context, db accessrepo.DBTX, organizationID string, principalURNs []string) ([]Grant, error) {
	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("query principal grants: %w", err)
	}

	grantRows := make([]Grant, 0, len(rows))
	for _, row := range rows {
		selectors, err := SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, fmt.Errorf("unmarshal grant selector: %w", err)
		}
		grantRows = append(grantRows, Grant{
			PrincipalUrn: row.PrincipalUrn.String(),
			Scope:        Scope(row.Scope),
			Selector:     selectors,
			Inferred:     row.Inferred,
		})
	}
	return grantRows, nil
}

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
	standardRoleSlugs := make(map[string]string)
	var standardRoleURNs []string
	for _, row := range rows {
		principal, err := urn.ParsePrincipal(row.PrincipalUrn)
		if err != nil {
			return nil, fmt.Errorf("parse user policy principal: %w", err)
		}
		key := principal.String()
		if _, ok := principalUsers[key]; !ok {
			principals = append(principals, principal)
			if IsStandardRole(row.GlobalRoleSlug) {
				standardRoleSlugs[key] = row.GlobalRoleSlug
				standardRoleURNs = append(standardRoleURNs, key)
			}
		}
		principalUsers[key] = append(principalUsers[key], row.UserID)
	}
	principalURNs, err := principalURNStrings(principals)
	if err != nil {
		return nil, err
	}
	grants, err := loadStoredGrants(ctx, db, organizationID, principalURNs)
	if err != nil {
		return nil, err
	}
	for _, roleURN := range standardRoleURNs {
		grants = append(grants, StandardRoleGrants(roleURN, standardRoleSlugs[roleURN])...)
	}
	for _, grant := range grants {
		for _, userID := range principalUsers[grant.PrincipalUrn] {
			result[userID] = append(result[userID], grant)
		}
	}
	return result, nil
}
