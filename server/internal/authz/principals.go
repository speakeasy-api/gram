package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func DeleteRoleGrants(ctx context.Context, q *repo.Queries, orgID, roleSlug, rolePrincipalURN string) error {
	roleIdentity, err := newRoleGrantIdentity(roleSlug, rolePrincipalURN)
	if err != nil {
		return err
	}

	return deleteRoleGrantPrincipals(ctx, q, orgID, roleIdentity)
}

func RolePrincipals(roleSlug, rolePrincipalURN string) ([]urn.Principal, error) {
	// TODO(AGE-1954): remove dual-read after legacy role:<slug> grants are backfilled.
	// During the role-principal migration, reads include both the canonical
	// role:<kind>:<uuid> principal and the legacy role:<slug> principal.
	roleIdentity, err := newRoleGrantIdentity(roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, err
	}

	return roleIdentity.MatchPrincipals, nil
}

func deleteRoleGrantPrincipals(ctx context.Context, q *repo.Queries, orgID string, roleIdentity roleGrantIdentity) error {
	for _, principal := range roleIdentity.MatchPrincipals {
		if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
			OrganizationID: orgID,
			PrincipalUrn:   principal,
		}); err != nil {
			return fmt.Errorf("delete grants for role %q: %w", roleIdentity.Slug, err)
		}
	}

	return nil
}

type roleGrantIdentity struct {
	Slug            string
	WritePrincipal  urn.Principal
	MatchPrincipals []urn.Principal
}

func resolveRoleGrantIdentity(ctx context.Context, dbtx repo.DBTX, orgID, roleSlug, rolePrincipalURN string) (roleGrantIdentity, error) {
	if rolePrincipalURN != "" {
		return newRoleGrantIdentity(roleSlug, rolePrincipalURN)
	}

	role, err := repo.New(dbtx).GetActiveOrganizationRoleBySlug(ctx, repo.GetActiveOrganizationRoleBySlugParams{
		OrganizationID: orgID,
		WorkosSlug:     roleSlug,
	})
	if err != nil {
		return roleGrantIdentity{}, fmt.Errorf("resolve role principal for %q: %w", roleSlug, err)
	}

	return newRoleGrantIdentity(roleSlug, role.RoleUrn)
}

func newRoleGrantIdentity(roleSlug, rolePrincipalURN string) (roleGrantIdentity, error) {
	// TODO(AGE-1954): drop legacy role:<slug> principals after the role-principal backfill is complete.
	writePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)
	matchPrincipals := make([]urn.Principal, 0, 2)

	if rolePrincipalURN != "" {
		principal, err := urn.ParsePrincipal(rolePrincipalURN)
		if err != nil {
			return roleGrantIdentity{}, fmt.Errorf("parse role principal urn %q: %w", rolePrincipalURN, err)
		}
		writePrincipal = principal
		matchPrincipals = append(matchPrincipals, principal)
	}

	legacyPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)
	if rolePrincipalURN == "" || legacyPrincipal.String() != rolePrincipalURN {
		matchPrincipals = append(matchPrincipals, legacyPrincipal)
	}

	return roleGrantIdentity{
		Slug:            roleSlug,
		WritePrincipal:  writePrincipal,
		MatchPrincipals: matchPrincipals,
	}, nil
}
