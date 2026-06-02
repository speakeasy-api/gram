package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
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

	q := repo.New(db)
	isMember, err := q.HasActiveOrganizationUser(ctx, repo.HasActiveOrganizationUserParams{
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

	roleRows, err := q.ListMemberRolePrincipalsByUser(ctx, repo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve role principals: %w", err)
	}

	for _, role := range roleRows {
		rp, err := newRolePrincipals(role.RoleSlug, role.PrincipalUrn)
		if err != nil {
			return nil, err
		}
		for _, principal := range rp.MatchPrincipals {
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

func DeleteRoleGrants(ctx context.Context, q *repo.Queries, orgID, roleSlug, rolePrincipalURN string) error {
	rp, err := newRolePrincipals(roleSlug, rolePrincipalURN)
	if err != nil {
		return err
	}

	return rp.deleteAllGrants(ctx, q, orgID)
}

// rolePrincipals owns the canonical write principal and all principals that
// may still match existing role grants. The WritePrincipal/MatchPrincipals
// split exists only for the AGE-1954 role-principal migration: new writes use
// WritePrincipal while reads/deletes still include legacy role:<slug> rows.
type rolePrincipals struct {
	Slug            string
	WritePrincipal  urn.Principal
	MatchPrincipals []urn.Principal
}

func loadRolePrincipals(ctx context.Context, dbtx repo.DBTX, orgID, roleSlug, rolePrincipalURN string) (rolePrincipals, error) {
	if rolePrincipalURN != "" {
		return newRolePrincipals(roleSlug, rolePrincipalURN)
	}

	role, err := repo.New(dbtx).GetActiveOrganizationRoleBySlug(ctx, repo.GetActiveOrganizationRoleBySlugParams{
		OrganizationID: orgID,
		WorkosSlug:     roleSlug,
	})
	if err != nil {
		return rolePrincipals{}, fmt.Errorf("resolve role principal for %q: %w", roleSlug, err)
	}

	return newRolePrincipals(roleSlug, role.RoleUrn)
}

func newRolePrincipals(roleSlug, rolePrincipalURN string) (rolePrincipals, error) {
	// TODO(AGE-1954): drop legacy role:<slug> principals after the role-principal backfill is complete.
	writePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)
	matchPrincipals := make([]urn.Principal, 0, 2)

	if rolePrincipalURN != "" {
		principal, err := urn.ParsePrincipal(rolePrincipalURN)
		if err != nil {
			return rolePrincipals{}, fmt.Errorf("parse role principal urn %q: %w", rolePrincipalURN, err)
		}
		writePrincipal = principal
		matchPrincipals = append(matchPrincipals, principal)
	}

	legacyPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)
	if rolePrincipalURN == "" || legacyPrincipal.String() != rolePrincipalURN {
		matchPrincipals = append(matchPrincipals, legacyPrincipal)
	}

	return rolePrincipals{
		Slug:            roleSlug,
		WritePrincipal:  writePrincipal,
		MatchPrincipals: matchPrincipals,
	}, nil
}

func (rp rolePrincipals) deleteAllGrants(ctx context.Context, q *repo.Queries, orgID string) error {
	for _, principal := range rp.MatchPrincipals {
		if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
			OrganizationID: orgID,
			PrincipalUrn:   principal,
		}); err != nil {
			return fmt.Errorf("delete grants for role %q: %w", rp.Slug, err)
		}
	}

	return nil
}

func (rp rolePrincipals) upsertGrants(ctx context.Context, q *repo.Queries, orgID string, rows []roleGrantRow) error {
	for _, row := range rows {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: orgID,
			PrincipalUrn:   rp.WritePrincipal,
			Scope:          string(row.Scope),
			Effect:         row.Effect.pgText(),
			Selectors:      row.SelectorRaw,
		}); err != nil {
			return fmt.Errorf("upsert grant %q for role %q: %w", row.Scope, rp.Slug, err)
		}
	}

	return nil
}

func (rp rolePrincipals) insertGrantsIfAbsent(ctx context.Context, q *repo.Queries, orgID string, rows []roleGrantRow) error {
	for _, row := range rows {
		if _, err := q.InsertPrincipalGrantIfAbsent(ctx, repo.InsertPrincipalGrantIfAbsentParams{
			OrganizationID: orgID,
			PrincipalUrn:   rp.WritePrincipal,
			Scope:          string(row.Scope),
			Effect:         row.Effect.pgText(),
			Selectors:      row.SelectorRaw,
		}); err != nil {
			return fmt.Errorf("insert grant %q for role %q: %w", row.Scope, rp.Slug, err)
		}
	}

	return nil
}

func (rp rolePrincipals) deleteGrants(ctx context.Context, q *repo.Queries, orgID string, rows []roleGrantRow) error {
	for _, principal := range rp.MatchPrincipals {
		for _, row := range rows {
			if _, err := q.DeletePrincipalGrantByIdentity(ctx, repo.DeletePrincipalGrantByIdentityParams{
				OrganizationID: orgID,
				PrincipalUrn:   principal,
				Scope:          string(row.Scope),
				Effect:         string(row.Effect),
				Selectors:      row.SelectorRaw,
			}); err != nil {
				return fmt.Errorf("delete grant %q for role %q: %w", row.Scope, rp.Slug, err)
			}
		}
	}

	return nil
}

func (rp rolePrincipals) toGrants(rows []roleGrantRow) []Grant {
	grants := make([]Grant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, Grant{
			PrincipalUrn: rp.WritePrincipal.String(),
			Scope:        row.Scope,
			Effect:       row.Effect,
			Selector:     row.Selector,
		})
	}

	return grants
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
