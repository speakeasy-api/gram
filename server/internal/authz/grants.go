package authz

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const WildcardResource = "*"

const (
	SystemRoleAdmin  = "admin"
	SystemRoleMember = "member"
)

type RoleGrant struct {
	Scope     string
	Resources []string
}

// SystemRoleGrants defines the canonical grant sets for the built-in system
// roles. These are seeded when RBAC is enabled and replace any existing grants
// for these roles (idempotent, won't clobber custom roles).
var SystemRoleGrants = map[string][]*RoleGrant{
	SystemRoleAdmin: {
		{Scope: string(ScopeOrgAdmin)},
		{Scope: string(ScopeOrgRead)},
		{Scope: string(ScopeProjectRead)},
		{Scope: string(ScopeProjectWrite)},
		{Scope: string(ScopeMCPRead)},
		{Scope: string(ScopeMCPWrite)},
		{Scope: string(ScopeMCPConnect)},
	},
	SystemRoleMember: {
		{Scope: string(ScopeOrgRead)},
		{Scope: string(ScopeProjectRead)},
		{Scope: string(ScopeMCPRead)},
		{Scope: string(ScopeMCPConnect)},
	},
}

// SeedSystemRoleGrants upserts the fixed grant sets for all system roles.
func SeedSystemRoleGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	for roleSlug, grants := range SystemRoleGrants {
		if err := SyncGrants(ctx, logger, db, organizationID, roleSlug, grants); err != nil {
			return fmt.Errorf("seed %s grants: %w", roleSlug, err)
		}
	}
	return nil
}

type Grant struct {
	Scope    Scope
	Selector Selector
}

type ScopedGrant struct {
	Scope     string
	SubScopes []string
	Resources []string
}

func grantsSatisfy(grants []Grant, checks []Check) bool {
	for _, grant := range grants {
		for _, check := range checks {
			if grant.Scope == check.Scope && grant.Selector.Matches(check.selector()) {
				return true
			}
		}
	}
	return false
}

func SyncGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, grants []*RoleGrant) error {
	if orgID == "" {
		return fmt.Errorf("organization id is required")
	}

	principalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin grant sync transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)

	if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: orgID,
		PrincipalUrn:   principalURN,
	}); err != nil {
		return fmt.Errorf("delete grants for role %q: %w", roleSlug, err)
	}

	for _, grant := range grants {
		if grant == nil {
			continue
		}

		if grant.Resources == nil {
			selectors, err := ForResource(WildcardResource).MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal wildcard selector for %q: %w", grant.Scope, err)
			}
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Resource:       WildcardResource,
				Selectors:      selectors,
			}); err != nil {
				return fmt.Errorf("upsert unrestricted grant %q for role %q: %w", grant.Scope, roleSlug, err)
			}

			continue
		}

		for _, resource := range grant.Resources {
			selectors, err := ForResource(resource).MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal selector for resource %q: %w", resource, err)
			}
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Resource:       resource,
				Selectors:      selectors,
			}); err != nil {
				return fmt.Errorf("upsert grant %q on resource %q for role %q: %w", grant.Scope, resource, roleSlug, err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit grant sync transaction: %w", err)
	}

	return nil
}

func GrantsForRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string) ([]*ScopedGrant, error) {
	rows, err := repo.New(db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug).String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, logger)
	}

	grantRows := make([]Grant, 0, len(rows))
	for _, row := range rows {
		selectors, err := selectorFromRow(row.Selectors, row.Resource)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "unmarshal grant selector").Log(ctx, logger)
		}
		grantRows = append(grantRows, Grant{
			Scope:    Scope(row.Scope),
			Selector: selectors,
		})
	}

	return GrantsFromRows(grantRows), nil
}

type scopeAgg struct {
	unrestricted bool
	resources    []string
}

func GrantsFromRows(rows []Grant) []*ScopedGrant {
	byScope := make(map[string]*scopeAgg)
	for _, row := range rows {
		scope := string(row.Scope)
		agg, ok := byScope[scope]
		if !ok {
			agg = &scopeAgg{unrestricted: false, resources: nil}
			byScope[scope] = agg
		}
		resourceID := row.Selector.ResourceID()
		if resourceID == WildcardResource {
			agg.unrestricted = true
			agg.resources = nil
			continue
		}
		if !agg.unrestricted {
			agg.resources = append(agg.resources, resourceID)
		}
	}

	scopes := make([]string, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)

	grants := make([]*ScopedGrant, 0, len(byScope))
	for _, scope := range scopes {
		agg := byScope[scope]
		if !agg.unrestricted {
			slices.Sort(agg.resources)
		}

		// also include the sub scopes that are granted by the primary scope
		// by looking up the scope expansions map
		scopeEnum := Scope(scope)

		// The expansions map is reversed in the sense that the key is the lower privilege scope and the value is the higher privilege scopes that also satisfy it, so we need to reverse it.
		subScopes := CalculateSubScopes(scopeEnum)

		grant := &ScopedGrant{Scope: scope, SubScopes: subScopes, Resources: nil}
		if agg.unrestricted {
			grant.Resources = nil
		} else {
			grant.Resources = append([]string(nil), agg.resources...)
		}
		grants = append(grants, grant)
	}

	return grants
}
