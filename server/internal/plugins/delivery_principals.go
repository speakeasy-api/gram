package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// ResolveDeliveryPrincipals returns the principal URNs used to decide which
// plugins are delivered to one person. Email and wildcard assignments apply
// even when userID is empty. A known active member additionally receives their
// user, role, and user:all principals. Directory group and attribute audiences
// are resolved from the normalized email in both cases.
func ResolveDeliveryPrincipals(ctx context.Context, db database.DBTX, organizationID, email, userID string) ([]string, error) {
	normalizedEmail := conv.NormalizeEmail(email)
	emailPrincipal, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + normalizedEmail)
	if err != nil {
		return nil, fmt.Errorf("parse plugin delivery email principal: %w", err)
	}

	principals := []string{emailPrincipal.String(), urn.PrincipalWildcard}
	directoryAudiences, err := ResolveDirectoryAudiencePrincipalsByEmails(ctx, db, organizationID, []string{normalizedEmail})
	if err != nil {
		return nil, err
	}
	principals = append(principals, directoryAudiences[normalizedEmail]...)

	if userID == "" {
		user, err := usersrepo.New(db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
			Email: normalizedEmail, OrganizationID: organizationID,
		})
		switch {
		case err == nil:
			userID = user.ID
		case errors.Is(err, pgx.ErrNoRows):
			// An unlinked address still receives email, wildcard, and directory
			// assignments, matching the device-agent delivery contract.
		default:
			return nil, fmt.Errorf("resolve plugin delivery user: %w", err)
		}
	}
	if userID != "" {
		resolved, err := authz.ResolveUserPrincipals(ctx, db, organizationID, userID)
		if err != nil {
			return nil, fmt.Errorf("resolve plugin delivery user principals: %w", err)
		}
		for _, principal := range resolved {
			principals = append(principals, principal.String())
		}
	}

	seen := make(map[string]struct{}, len(principals))
	unique := make([]string, 0, len(principals))
	for _, principal := range principals {
		if _, ok := seen[principal]; ok {
			continue
		}
		seen[principal] = struct{}{}
		unique = append(unique, principal)
	}
	return unique, nil
}
