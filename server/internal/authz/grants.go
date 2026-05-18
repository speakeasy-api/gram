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
	Selectors []Selector
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
		{Scope: string(ScopeEnvironmentRead)},
		{Scope: string(ScopeEnvironmentWrite)},
	},
	SystemRoleMember: {
		{Scope: string(ScopeOrgRead)},
		{Scope: string(ScopeProjectRead)},
		{Scope: string(ScopeMCPRead)},
		{Scope: string(ScopeMCPConnect)},
		{Scope: string(ScopeEnvironmentRead)},
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
	PrincipalUrn string
	Scope        Scope
	Selector     Selector
}

type ScopedGrant struct {
	Scope     string
	SubScopes []string
	Selectors []Selector
}

func SyncGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, grants []*RoleGrant) error {
	if orgID == "" {
		return fmt.Errorf("organization id is required")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin grant sync transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if _, err := SyncGrantsTx(ctx, tx, orgID, roleSlug, grants); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit grant sync transaction: %w", err)
	}

	return nil
}

func SyncGrantsTx(ctx context.Context, dbtx repo.DBTX, orgID string, roleSlug string, grants []*RoleGrant) ([]*ScopedGrant, error) {
	if orgID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	principalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	q := repo.New(dbtx)

	if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: orgID,
		PrincipalUrn:   principalURN,
	}); err != nil {
		return nil, fmt.Errorf("delete grants for role %q: %w", roleSlug, err)
	}

	for _, grant := range grants {
		if grant == nil {
			continue
		}

		scope := Scope(grant.Scope)

		// nil selectors = unrestricted (wildcard) access for this scope.
		// Empty non-nil slice ([]Selector{}) = no grant rows (no access).
		if grant.Selectors == nil {
			sel := NewSelector(scope, WildcardResource)
			selBytes, err := sel.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("marshal wildcard selector for %q: %w", grant.Scope, err)
			}
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Selectors:      selBytes,
			}); err != nil {
				return nil, fmt.Errorf("upsert unrestricted grant %q for role %q: %w", grant.Scope, roleSlug, err)
			}
			continue
		}

		for _, sel := range grant.Selectors {
			if err := ValidateSelector(scope, sel); err != nil {
				return nil, fmt.Errorf("invalid selector for scope %q: %w", grant.Scope, err)
			}

			selBytes, err := sel.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("marshal selector for scope %q: %w", grant.Scope, err)
			}
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Selectors:      selBytes,
			}); err != nil {
				return nil, fmt.Errorf("upsert grant %q for role %q: %w", grant.Scope, roleSlug, err)
			}
		}
	}

	rows, err := q.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   principalURN.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list synced grants for role %q: %w", roleSlug, err)
	}

	scoped, err := scopedGrantsFromRows(principalURN.String(), rows)
	if err != nil {
		return nil, fmt.Errorf("load synced grants for role %q: %w", roleSlug, err)
	}

	return scoped, nil
}

func GrantsForRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string) ([]*ScopedGrant, error) {
	rows, err := repo.New(db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug).String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, logger)
	}

	scoped, err := scopedGrantsFromRows(urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug).String(), rows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unmarshal grant selector").Log(ctx, logger)
	}

	return scoped, nil
}

func scopedGrantsFromRows(rolePrincipalURN string, rows []repo.ListPrincipalGrantsByOrgRow) ([]*ScopedGrant, error) {
	grantRows := make([]Grant, 0, len(rows))
	for _, row := range rows {
		selectors, err := SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, err
		}
		grantRows = append(grantRows, Grant{
			PrincipalUrn: rolePrincipalURN,
			Scope:        Scope(row.Scope),
			Selector:     selectors,
		})
	}

	return GrantsToScopedGrants(grantRows), nil
}

type scopeAgg struct {
	unrestricted bool
	selectors    []Selector
}

// GrantsToScopedGrants groups raw grants by scope, collapsing wildcards.
func GrantsToScopedGrants(rows []Grant) []*ScopedGrant {
	byScope := make(map[string]*scopeAgg)
	for _, row := range rows {
		scope := string(row.Scope)
		agg, ok := byScope[scope]
		if !ok {
			agg = &scopeAgg{unrestricted: false, selectors: nil}
			byScope[scope] = agg
		}
		resourceID := row.Selector.ResourceID()
		if resourceID == WildcardResource && len(row.Selector) <= 2 {
			// Pure wildcard: {"resource_kind":"*","resource_id":"*"} or similar.
			agg.unrestricted = true
			agg.selectors = nil
			continue
		}
		if !agg.unrestricted {
			agg.selectors = append(agg.selectors, row.Selector)
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
		subScopes := CalculateSubScopes(Scope(scope))

		grant := &ScopedGrant{Scope: scope, SubScopes: subScopes, Selectors: nil}
		if !agg.unrestricted {
			grant.Selectors = append([]Selector(nil), agg.selectors...)
		}
		grants = append(grants, grant)
	}

	return grants
}

// findMatchingGrant compares a list of grants against a list of checks and returns
// the first grant / check tuple that is satisfied.
func findMatchingGrant(grants []Grant, checks []Check) (*Grant, *Check) {
	for _, grant := range grants {
		for _, check := range checks {
			if grant.Scope == check.Scope && grant.Selector.Matches(check.selector()) {
				g, c := grant, check
				return &g, &c
			}
		}
	}

	return nil, nil
}
