package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
)

// ResolveOrganizationAdminEmails returns the distinct email addresses of
// active organization members whose effective grants satisfy org:admin.
// Successfully resolved recipients are returned alongside any per-user
// resolution errors so notification callers can deliver to the partial
// audience and still retry the failed resolution.
func ResolveOrganizationAdminEmails(ctx context.Context, db repo.DBTX, organizationID string) ([]string, error) {
	users, err := repo.New(db).ListAccessNotificationUsers(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization notification users: %w", err)
	}

	check := Check{
		Scope:         ScopeOrgAdmin,
		ResourceKind:  "",
		ResourceID:    organizationID,
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
	}
	recipients := make([]string, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	var resolutionErrors []error
	for _, user := range users {
		principals, err := ResolveUserPrincipals(ctx, db, organizationID, user.ID)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve principals for organization user %q: %w", user.ID, err))
			continue
		}
		grants, err := LoadGrants(ctx, db, organizationID, principals)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("load grants for organization user %q: %w", user.ID, err))
			continue
		}
		if !GrantsSatisfy(grants, check) {
			continue
		}

		emailKey := strings.ToLower(user.Email)
		if _, ok := seen[emailKey]; ok {
			continue
		}
		seen[emailKey] = struct{}{}
		recipients = append(recipients, user.Email)
	}

	return recipients, errors.Join(resolutionErrors...)
}
