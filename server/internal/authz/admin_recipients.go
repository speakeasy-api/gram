package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
)

// ResolveOrganizationAdminEmails returns the distinct email addresses of
// active organization members whose effective grants satisfy org:admin.
// Successfully resolved recipients are returned alongside any per-user
// resolution errors so notification callers can deliver to the partial
// audience and still retry the failed resolution.
func ResolveOrganizationAdminEmails(ctx context.Context, db repo.DBTX, organizationID string) ([]string, error) {
	check := Check{
		Scope:           ScopeOrgAdmin,
		ResourceKind:    "",
		ResourceID:      organizationID,
		Dimensions:      nil,
		selectorMatch:   selectorMatchNormal,
		representedUser: "",
	}
	return resolveOrganizationAudience(ctx, db, organizationID, func(_ string, grants []Grant) (bool, error) {
		evaluation, err := evaluateGrantCheck(grants, check)
		if err != nil {
			return false, err
		}
		return evaluation.Grant != nil, nil
	})
}

// resolveOrganizationAudience returns the distinct email addresses of active
// organization members whose live grants allows accepts, with any per-member
// resolution errors.
func resolveOrganizationAudience(ctx context.Context, db repo.DBTX, organizationID string, allows func(userID string, grants []Grant) (bool, error)) ([]string, error) {
	users, err := repo.New(db).ListAccessNotificationUsers(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization notification users: %w", err)
	}

	type recipient struct {
		email  string
		userID string
	}
	byEmail := make(map[string]recipient, len(users))
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
		allowed, err := allows(user.ID, grants)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("evaluate grants for organization user %q: %w", user.ID, err))
			continue
		}
		if !allowed {
			continue
		}

		emailKey := strings.ToLower(user.Email)
		current, ok := byEmail[emailKey]
		if ok && current.userID <= user.ID {
			continue
		}
		byEmail[emailKey] = recipient{email: user.Email, userID: user.ID}
	}

	emailKeys := make([]string, 0, len(byEmail))
	for emailKey := range byEmail {
		emailKeys = append(emailKeys, emailKey)
	}
	slices.Sort(emailKeys)
	recipients := make([]string, 0, len(emailKeys))
	for _, emailKey := range emailKeys {
		recipients = append(recipients, byEmail[emailKey].email)
	}

	return recipients, errors.Join(resolutionErrors...)
}
