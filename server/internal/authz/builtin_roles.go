package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const globalRolePrincipalPrefix = "role:global:"

// ErrLockedRoleGrants reports an attempt to modify permissions on a locked
// Speakeasy-local built-in role (super_admin, standard_admin, standard_member).
var ErrLockedRoleGrants = errors.New("built-in role permissions are locked and cannot be changed")

// LocalBuiltInRole describes a built-in role owned by Speakeasy rather than
// synced from WorkOS. Its row lives in global_roles, and WorkOS role webhooks
// never overwrite or delete it.
type LocalBuiltInRole struct {
	// Slug is the immutable identifier stored in global_roles.workos_slug.
	Slug string

	// Name is the display name shown in the dashboard and API.
	Name string

	// Description explains the role's purpose in plain words.
	Description string
}

var localBuiltInRoles = []LocalBuiltInRole{
	{
		Slug:        SystemRoleSuperAdmin,
		Name:        "Super Admin",
		Description: "Break-glass organization administrator",
	},
	{
		Slug:        SystemRoleStandardAdmin,
		Name:        "Standard Admin",
		Description: "Built-in administrator role kept up to date by Speakeasy",
	},
	{
		Slug:        SystemRoleStandardMember,
		Name:        "Standard Member",
		Description: "Built-in member role kept up to date by Speakeasy",
	},
}

// LocalBuiltInRoles returns a copy of the Speakeasy-local built-in role
// definitions.
func LocalBuiltInRoles() []LocalBuiltInRole {
	return slices.Clone(localBuiltInRoles)
}

// IsLocalBuiltInRole reports whether slug names one of the Speakeasy-local
// built-in roles (super_admin, standard_admin, standard_member).
func IsLocalBuiltInRole(slug string) bool {
	switch slug {
	case SystemRoleSuperAdmin, SystemRoleStandardAdmin, SystemRoleStandardMember:
		return true
	default:
		return false
	}
}

// IsStandardRole reports whether slug names one of the locked standard roles
// (standard_admin, standard_member) whose permissions come from
// registry.Definitions rather than stored principal_grants rows.
func IsStandardRole(slug string) bool {
	return slug == SystemRoleStandardAdmin || slug == SystemRoleStandardMember
}

// LockedRolePrincipals returns the subset of principalURNs that identify a
// locked Speakeasy-local built-in role (super_admin, standard_admin,
// standard_member), keyed by principal URN with the role slug as value. It
// issues no database query when no global role URNs are present.
func LockedRolePrincipals(ctx context.Context, db repo.DBTX, principalURNs []string) (map[string]string, error) {
	slugByURN, _, err := resolveGlobalRoleSlugs(ctx, db, principalURNs)
	if err != nil {
		return nil, err
	}
	var locked map[string]string
	for roleURN, slug := range slugByURN {
		if !IsLocalBuiltInRole(slug) {
			continue
		}
		if locked == nil {
			locked = make(map[string]string)
		}
		locked[roleURN] = slug
	}
	return locked, nil
}

// IsLockedRolePrincipal reports whether principal identifies one of the locked
// Speakeasy-local built-in roles. Non-global principals issue no database query.
func IsLockedRolePrincipal(ctx context.Context, db repo.DBTX, principal urn.Principal) (string, bool, error) {
	if principal.Type != urn.PrincipalTypeRole {
		return "", false, nil
	}
	principalURN := principal.String()
	if !strings.HasPrefix(principalURN, globalRolePrincipalPrefix) {
		return "", false, nil
	}
	locked, err := LockedRolePrincipals(ctx, db, []string{principalURN})
	if err != nil {
		return "", false, err
	}
	slug, ok := locked[principalURN]
	return slug, ok, nil
}

// StandardRoleScopes returns the canonical scopes for a standard role, or nil
// for any other role slug (including super_admin, whose bypass is not
// represented as role grants).
func StandardRoleScopes(slug string) []Scope {
	if !IsStandardRole(slug) {
		return nil
	}
	return slices.Clone(systemRoleScopes[slug])
}

// StandardRoleHasScope reports whether slug is a standard role whose canonical
// scope list includes scope.
func StandardRoleHasScope(slug string, scope Scope) bool {
	if !IsStandardRole(slug) {
		return false
	}
	return slices.Contains(systemRoleScopes[slug], scope)
}

// StandardRoleGrants builds unrestricted wildcard Grant values for a standard
// role principal, or nil for any other role slug.
func StandardRoleGrants(rolePrincipalURN, slug string) []Grant {
	if !IsStandardRole(slug) {
		return nil
	}
	scopes := systemRoleScopes[slug]
	grants := make([]Grant, 0, len(scopes))
	for _, scope := range scopes {
		grant := NewGrant(scope, WildcardResource)
		grant.PrincipalUrn = rolePrincipalURN
		grants = append(grants, grant)
	}
	return grants
}

// EnsureLocalBuiltInGlobalRoles idempotently ensures that the Speakeasy-local
// built-in roles exist in global_roles without emitting role-distribution
// fanout requests.
func EnsureLocalBuiltInGlobalRoles(ctx context.Context, db repo.DBTX) error {
	q := repo.New(db)
	now := conv.ToPGTimestamptz(time.Now().UTC())
	for _, role := range localBuiltInRoles {
		if err := q.EnsureLocalGlobalRole(ctx, repo.EnsureLocalGlobalRoleParams{
			WorkosSlug:        role.Slug,
			WorkosName:        role.Name,
			WorkosDescription: conv.ToPGTextEmpty(role.Description),
			WorkosCreatedAt:   now,
			WorkosUpdatedAt:   now,
		}); err != nil {
			return fmt.Errorf("ensure local built-in global role %q: %w", role.Slug, err)
		}
	}
	return nil
}

// LoadStandardRoleGrants resolves any active global standard role URNs in
// principalURNs and returns their wildcard grants. Non-global principals and
// other global roles produce no grants and issue no database query when no
// global role URNs are present.
func LoadStandardRoleGrants(ctx context.Context, db repo.DBTX, principalURNs []string) ([]Grant, error) {
	slugByURN, orderedURNs, err := resolveGlobalRoleSlugs(ctx, db, principalURNs)
	if err != nil {
		return nil, err
	}
	if len(slugByURN) == 0 {
		return nil, nil
	}

	var grants []Grant
	for _, roleURN := range orderedURNs {
		slug, ok := slugByURN[roleURN]
		if !ok || !IsStandardRole(slug) {
			continue
		}
		grants = append(grants, StandardRoleGrants(roleURN, slug)...)
	}
	return grants, nil
}

// StandardRoleScopesFor returns the deduplicated scope strings granted by
// standard roles among principals. Raw SQL list queries pass this set alongside
// stored principal_grants rows so registry.Definitions stays the single source
// of truth for standard role scopes.
func StandardRoleScopesFor(ctx context.Context, db repo.DBTX, principals []urn.Principal) ([]string, error) {
	if len(principals) == 0 {
		return nil, nil
	}
	urns := make([]string, 0, len(principals))
	for _, p := range principals {
		urns = append(urns, p.String())
	}
	slugByURN, orderedURNs, err := resolveGlobalRoleSlugs(ctx, db, urns)
	if err != nil {
		return nil, err
	}
	if len(slugByURN) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{})
	var scopes []string
	for _, roleURN := range orderedURNs {
		slug, ok := slugByURN[roleURN]
		if !ok || !IsStandardRole(slug) {
			continue
		}
		for _, scope := range systemRoleScopes[slug] {
			name := string(scope)
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			scopes = append(scopes, name)
		}
	}
	return scopes, nil
}

func resolveGlobalRoleSlugs(ctx context.Context, db repo.DBTX, principalURNs []string) (map[string]string, []string, error) {
	var ids []uuid.UUID
	var orderedURNs []string
	urnByID := make(map[uuid.UUID]string)
	for _, raw := range principalURNs {
		idStr, ok := strings.CutPrefix(raw, globalRolePrincipalPrefix)
		if !ok {
			continue
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		if _, exists := urnByID[id]; exists {
			continue
		}
		urnByID[id] = raw
		ids = append(ids, id)
		orderedURNs = append(orderedURNs, raw)
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}

	rows, err := repo.New(db).ListGlobalRoleSlugsByIDs(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("list global role slugs: %w", err)
	}
	slugByURN := make(map[string]string, len(rows))
	for _, row := range rows {
		if roleURN, ok := urnByID[row.ID]; ok {
			slugByURN[roleURN] = row.WorkosSlug
		}
	}
	return slugByURN, orderedURNs, nil
}
