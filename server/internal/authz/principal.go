package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrPrincipalNotFound reports that a principal does not resolve to an active
// target in the organization.
var ErrPrincipalNotFound = errors.New("principal not found")

// ErrPrincipalInvalid reports caller input that cannot identify a supported
// principal.
var ErrPrincipalInvalid = errors.New("principal invalid")

// ResolveUserPrincipals resolves the principals that should be considered for
// an organization-scoped request. Every request with a known organization gets
// user:all. When userID identifies an active organization member, the result
// also includes user:<id> plus assigned role principals. Missing, empty, or
// cross-org users still receive only user:all.
func ResolveUserPrincipals(ctx context.Context, db repo.DBTX, organizationID string, userID string) ([]urn.Principal, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}
	if userID == urn.AllUsersPrincipalID {
		return nil, fmt.Errorf("%w: user id %q", ErrPrincipalInvalid, userID)
	}

	principals := []urn.Principal{AllUsersPrincipal()}
	if userID == "" {
		return principals, nil
	}

	isMember, err := orgrepo.New(db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve organization user principal: %w", err)
	}
	if !isMember {
		return principals, nil
	}

	principals = append(principals, urn.NewPrincipal(urn.PrincipalTypeUser, userID))
	seen := map[string]struct{}{
		AllUsersPrincipal().String():                             {},
		urn.NewPrincipal(urn.PrincipalTypeUser, userID).String(): {},
	}

	q := repo.New(db)
	roleRows, err := q.ListMemberRolePrincipalsByUser(ctx, repo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve role principals: %w", err)
	}

	for _, role := range roleRows {
		principal, err := parseRolePrincipalURN(role.PrincipalUrn)
		if err != nil {
			return nil, err
		}
		key := principal.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		principals = append(principals, principal)
	}

	return principals, nil
}

// ValidatePrincipal verifies that principal is a valid grant target in the
// organization. Unlike ResolveUserPrincipals, this is strict: concrete users
// must be active organization members and role principals must identify an
// active role in the organization. Caller-input problems are reported via
// ErrPrincipalInvalid and ErrPrincipalNotFound; any other error is an
// infrastructure failure, not a verdict on the principal.
func ValidatePrincipal(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal) error {
	if organizationID == "" {
		return fmt.Errorf("organization id is required")
	}

	switch principal.Type {
	case urn.PrincipalTypeUser:
		if principal.String() == AllUsersPrincipal().String() {
			return nil
		}
		if principal.ID == "" {
			return fmt.Errorf("%w: user id %q", ErrPrincipalInvalid, principal.ID)
		}
		isMember, err := orgrepo.New(db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
			UserID:         principal.ID,
			OrganizationID: organizationID,
		})
		if err != nil {
			return fmt.Errorf("validate user principal %q: %w", principal.String(), err)
		}
		if !isMember {
			return fmt.Errorf("validate user principal %q: %w", principal.String(), ErrPrincipalNotFound)
		}
	case urn.PrincipalTypeRole:
		roleKind, rawRoleID, ok := strings.Cut(principal.ID, ":")
		if !ok {
			return fmt.Errorf("%w: role principal %q", ErrPrincipalInvalid, principal.String())
		}
		if roleKind != "organization" && roleKind != "global" {
			return fmt.Errorf("%w: role principal %q", ErrPrincipalInvalid, principal.String())
		}
		roleID, err := uuid.Parse(rawRoleID)
		if err != nil {
			return fmt.Errorf("%w: role principal %q: %w", ErrPrincipalInvalid, principal.String(), err)
		}
		row, err := repo.New(db).GetOrganizationRoleByID(ctx, repo.GetOrganizationRoleByIDParams{
			OrganizationID: organizationID,
			ID:             roleID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("validate role principal %q: %w", principal.String(), ErrPrincipalNotFound)
			}
			return fmt.Errorf("validate role principal %q: %w", principal.String(), err)
		}
		if row.RoleUrn != principal.String() {
			return fmt.Errorf("%w: role principal %q does not match active role %q", ErrPrincipalNotFound, principal.String(), row.RoleUrn)
		}
	default:
		return fmt.Errorf("%w: unsupported principal type %q", ErrPrincipalInvalid, principal.Type)
	}

	return nil
}

func DeleteRoleGrants(ctx context.Context, q *repo.Queries, orgID, rolePrincipalURN string) error {
	principal, err := parseRolePrincipalURN(rolePrincipalURN)
	if err != nil {
		return err
	}

	if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: orgID,
		PrincipalUrn:   principal,
	}); err != nil {
		return fmt.Errorf("delete grants for %s: %w", principal.Label(), err)
	}

	return nil
}

// PatchPrincipalGrants applies exact grant additions and removals for one
// principal without treating omitted grants as deletes.
func PatchPrincipalGrants(ctx context.Context, dbtx repo.DBTX, orgID string, principal urn.Principal, addGrants []*RoleGrant, removeGrants []*RoleGrant) error {
	if orgID == "" {
		return fmt.Errorf("organization id is required")
	}
	if _, err := principal.Value(); err != nil {
		return fmt.Errorf("invalid grant principal: %w", err)
	}

	removeRows, err := flattenRoleGrants(removeGrants)
	if err != nil {
		return err
	}
	q := repo.New(dbtx)
	if err := deletePrincipalGrants(ctx, q, orgID, principal, removeRows); err != nil {
		return err
	}

	addRows, err := flattenRoleGrants(addGrants)
	if err != nil {
		return err
	}
	if err := upsertPrincipalGrants(ctx, q, orgID, principal, addRows); err != nil {
		return err
	}

	return nil
}

func AllUsersPrincipal() urn.Principal {
	return urn.NewPrincipal(urn.PrincipalTypeUser, urn.AllUsersPrincipalID)
}

func loadRolePrincipal(ctx context.Context, dbtx repo.DBTX, orgID, roleSlug, rolePrincipalURN string) (urn.Principal, error) {
	if rolePrincipalURN == "" {
		role, err := repo.New(dbtx).GetActiveOrganizationRoleBySlug(ctx, repo.GetActiveOrganizationRoleBySlugParams{
			OrganizationID: orgID,
			WorkosSlug:     roleSlug,
		})
		if err != nil {
			return urn.Principal{}, fmt.Errorf("resolve role principal for %q: %w", roleSlug, err)
		}
		rolePrincipalURN = role.RoleUrn
	}

	return parseRolePrincipalURN(rolePrincipalURN)
}

func parseRolePrincipalURN(rolePrincipalURN string) (urn.Principal, error) {
	principal, err := urn.ParsePrincipal(rolePrincipalURN)
	if err != nil {
		return urn.Principal{}, fmt.Errorf("parse role principal urn %q: %w", rolePrincipalURN, err)
	}
	if principal.Type != urn.PrincipalTypeRole {
		return urn.Principal{}, fmt.Errorf("invalid role principal %q", rolePrincipalURN)
	}

	roleKind, rawRoleID, ok := strings.Cut(principal.ID, ":")
	if !ok || (roleKind != "organization" && roleKind != "global") {
		return urn.Principal{}, fmt.Errorf("invalid role principal %q", rolePrincipalURN)
	}
	if _, err := uuid.Parse(rawRoleID); err != nil {
		return urn.Principal{}, fmt.Errorf("invalid role principal %q: %w", rolePrincipalURN, err)
	}

	return principal, nil
}

func upsertPrincipalGrants(ctx context.Context, q *repo.Queries, orgID string, principal urn.Principal, rows []roleGrantRow) error {
	for _, row := range rows {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: orgID,
			PrincipalUrn:   principal,
			Scope:          string(row.Scope),
			Selectors:      row.SelectorRaw,
		}); err != nil {
			return fmt.Errorf("upsert grant %q for %s: %w", row.Scope, principal.Label(), err)
		}
	}

	return nil
}

func insertRoleGrantsIfAbsent(ctx context.Context, q *repo.Queries, orgID string, roleSlug string, principal urn.Principal, rows []roleGrantRow) error {
	for _, row := range rows {
		if _, err := q.InsertPrincipalGrantIfAbsent(ctx, repo.InsertPrincipalGrantIfAbsentParams{
			OrganizationID: orgID,
			PrincipalUrn:   principal,
			Scope:          string(row.Scope),
			Selectors:      row.SelectorRaw,
		}); err != nil {
			return fmt.Errorf("insert grant %q for role %q: %w", row.Scope, roleSlug, err)
		}
	}

	return nil
}

func deletePrincipalGrants(ctx context.Context, q *repo.Queries, orgID string, principal urn.Principal, rows []roleGrantRow) error {
	for _, row := range rows {
		if _, err := q.DeletePrincipalGrantByIdentity(ctx, repo.DeletePrincipalGrantByIdentityParams{
			OrganizationID: orgID,
			PrincipalUrn:   principal,
			Scope:          string(row.Scope),
			Selectors:      row.SelectorRaw,
		}); err != nil {
			return fmt.Errorf("delete grant %q for %s: %w", row.Scope, principal.Label(), err)
		}
	}

	return nil
}

func principalURNStrings(principals []urn.Principal) ([]string, error) {
	if len(principals) == 0 {
		return nil, fmt.Errorf("no principals provided")
	}

	seen := make(map[string]struct{}, len(principals))
	principalURNs := make([]string, 0, len(principals))
	for _, principal := range principals {
		text, err := principal.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("marshal principal urn %q: %w", principal.String(), err)
		}

		principalURN := string(text)

		if _, ok := seen[principalURN]; ok {
			continue
		}

		seen[principalURN] = struct{}{}
		principalURNs = append(principalURNs, principalURN)
	}

	return principalURNs, nil
}

func isAllUsersPrincipal(principal urn.Principal) bool {
	return principal.String() == AllUsersPrincipal().String()
}
