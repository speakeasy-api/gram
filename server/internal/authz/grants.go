package authz

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const WildcardResource = "*"

// allScopeGrants returns wildcard grants for every defined scope. Used to give
// superadmins (e.g. during org impersonation) unrestricted access.
func allScopeGrants() []Grant {
	scopes := []Scope{
		ScopeOrgRead, ScopeOrgAdmin,
		ScopeProjectRead, ScopeProjectWrite,
		ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect,
		ScopeEnvironmentRead, ScopeEnvironmentWrite,
	}
	grants := make([]Grant, 0, len(scopes))
	for _, s := range scopes {
		grants = append(grants, NewGrant(s, WildcardResource))
	}
	return grants
}

const (
	SystemRoleAdmin  = "admin"
	SystemRoleMember = "member"
)

// PolicyEffect determines whether a grant allows or denies the matched scope.
type PolicyEffect string

const (
	PolicyEffectAllow PolicyEffect = "allow"
	PolicyEffectDeny  PolicyEffect = "deny"
)

type RoleGrant struct {
	Scope     string
	Effect    PolicyEffect
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
	Effect       PolicyEffect
	Selector     Selector
}

type ScopedGrant struct {
	Scope     string
	Effect    PolicyEffect
	SubScopes []string
	Selectors []Selector
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

		scope := Scope(grant.Scope)
		effect := effectOrDefault(grant.Effect)

		// nil selectors = unrestricted (wildcard) access for this scope.
		// Empty non-nil slice ([]Selector{}) = no grant rows (no access).
		if grant.Selectors == nil {
			sel := NewSelector(scope, WildcardResource)
			selBytes, err := sel.MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal wildcard selector for %q: %w", grant.Scope, err)
			}
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Effect:         effectToPgtype(effect),
				Selectors:      selBytes,
			}); err != nil {
				return fmt.Errorf("upsert unrestricted grant %q for role %q: %w", grant.Scope, roleSlug, err)
			}
			continue
		}

		for _, sel := range grant.Selectors {
			if err := ValidateSelector(scope, sel); err != nil {
				return fmt.Errorf("invalid selector for scope %q: %w", grant.Scope, err)
			}

			selBytes, err := sel.MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal selector for scope %q: %w", grant.Scope, err)
			}
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Effect:         effectToPgtype(effect),
				Selectors:      selBytes,
			}); err != nil {
				return fmt.Errorf("upsert grant %q for role %q: %w", grant.Scope, roleSlug, err)
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

	rolePrincipalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug).String()

	grantRows := make([]Grant, 0, len(rows))
	for _, row := range rows {
		selectors, err := SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "unmarshal grant selector").Log(ctx, logger)
		}
		grantRows = append(grantRows, Grant{
			PrincipalUrn: rolePrincipalURN,
			Scope:        Scope(row.Scope),
			Effect:       effectFromNullable(row.Effect),
			Selector:     selectors,
		})
	}

	return GrantsToScopedGrants(grantRows), nil
}

// effectOrDefault returns the effect, defaulting to allow for backward
// compatibility with existing grants that have no explicit effect.
func effectOrDefault(e PolicyEffect) PolicyEffect {
	return conv.Default(e, PolicyEffectAllow)
}

// effectToPgtype converts a PolicyEffect to pgtype.Text for DB storage.
// Allow maps to NULL (backward compatible with existing rows).
// Deny maps to 'deny'.
func effectToPgtype(e PolicyEffect) pgtype.Text {
	effect := conv.Default(e, PolicyEffectAllow)
	if effect == PolicyEffectAllow {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: string(effect), Valid: true}
}

// effectFromNullable converts a nullable DB string to PolicyEffect.
// NULL or empty → allow.
func effectFromNullable(s pgtype.Text) PolicyEffect {
	if !s.Valid {
		return PolicyEffectAllow
	}
	return conv.Default(PolicyEffect(s.String), PolicyEffectAllow)
}

// scopeEffectKey groups grants by scope+effect for GrantsToScopedGrants.
type scopeEffectKey struct {
	scope  string
	effect PolicyEffect
}

type scopeAgg struct {
	unrestricted bool
	selectors    []Selector
}

// GrantsToScopedGrants groups raw grants by scope+effect, collapsing wildcards.
// TODO: simplify — this method is getting complex; consider breaking into
// smaller steps (grouping, wildcard collapsing, output building).
func GrantsToScopedGrants(rows []Grant) []*ScopedGrant {
	byKey := make(map[scopeEffectKey]*scopeAgg)
	for _, row := range rows {
		effect := effectOrDefault(row.Effect)
		key := scopeEffectKey{scope: string(row.Scope), effect: effect}
		agg, ok := byKey[key]
		if !ok {
			agg = &scopeAgg{unrestricted: false, selectors: nil}
			byKey[key] = agg
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

	keys := make([]scopeEffectKey, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b scopeEffectKey) int {
		if c := cmp.Compare(a.scope, b.scope); c != 0 {
			return c
		}
		return cmp.Compare(string(a.effect), string(b.effect))
	})

	grants := make([]*ScopedGrant, 0, len(byKey))
	for _, key := range keys {
		agg := byKey[key]
		subScopes := CalculateSubScopes(Scope(key.scope))

		grant := &ScopedGrant{Scope: key.scope, Effect: key.effect, SubScopes: subScopes, Selectors: nil}
		if !agg.unrestricted {
			grant.Selectors = append([]Selector(nil), agg.selectors...)
		}
		grants = append(grants, grant)
	}

	return grants
}

// evaluateGrants implements deny-wins semantics per the RFC:
//
//	permit(check) = at least one matching allow grant exists
//	                AND no matching deny grant exists
//
// Returns the first matching allow grant+check pair if permitted, nil otherwise.
// Also returns whether a deny was matched (for logging).
func evaluateGrants(grants []Grant, checks []Check) (allowGrant *Grant, allowCheck *Check, denied bool) {
	var firstAllow *Grant
	var firstAllowCheck *Check

	for i := range grants {
		grant := &grants[i]
		effect := effectOrDefault(grant.Effect)
		for j := range checks {
			check := &checks[j]
			checkSel := check.selector()
			if grant.Scope != check.Scope {
				continue
			}

			if effect == PolicyEffectDeny {
				// Deny only matches the original scope, not expanded parents.
				// Uses StrictMatches: every dimension in the deny selector must
				// be present in the check. This prevents a tool-scoped deny
				// from blocking a dimensionless server-level connect probe.
				if !check.expanded && grant.Selector.StrictMatches(checkSel) {
					return nil, nil, true
				}
				continue
			}

			// Allow: permissive matching (skip missing dimensions).
			if !grant.Selector.Matches(checkSel) {
				continue
			}
			if firstAllow == nil {
				firstAllow = grant
				firstAllowCheck = check
			}
		}
	}

	return firstAllow, firstAllowCheck, false
}

// findMatchingGrant compares a list of grants against a list of checks and returns
// the first grant / check tuple that is satisfied. Deny grants block the match.
func findMatchingGrant(grants []Grant, checks []Check) (*Grant, *Check) {
	allow, check, _ := evaluateGrants(grants, checks)
	return allow, check
}
