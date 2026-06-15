package authz

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	SystemRoleAdmin  = "admin"
	SystemRoleMember = "member"
	WildcardResource = "*"
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

// GrantsSatisfy reports whether the loaded grant set authorizes check.
func GrantsSatisfy(grants []Grant, check Check) bool {
	if err := validateInput(check); err != nil {
		return false
	}
	allowGrant, _, _ := evaluateGrants(grants, check.expand())
	return allowGrant != nil
}

// SystemRoleGrants defines the canonical grant sets for the built-in system
// roles. These are seeded when RBAC is enabled and replace any existing grants
// for these roles (idempotent, won't clobber custom roles).
var SystemRoleGrants = map[string][]*RoleGrant{
	SystemRoleAdmin:  roleGrantsForScopes(adminScopes),
	SystemRoleMember: roleGrantsForScopes(memberScopes),
}

// SeedSystemRoleGrants bootstraps the fixed grant sets for system roles once.
func SeedSystemRoleGrants(ctx context.Context, db *pgxpool.Pool, organizationID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin system role seed transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := seedSystemRoleGrantsTx(ctx, tx, organizationID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit system role seed transaction: %w", err)
	}

	return nil
}

func seedSystemRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	q := repo.New(dbtx)
	for roleSlug, grants := range SystemRoleGrants {
		existingRole, err := q.GetGlobalRoleBySlug(ctx, roleSlug)
		seedRole := false
		switch {
		case err == nil:
			seedRole = existingRole.Deleted
		case errors.Is(err, pgx.ErrNoRows):
			seedRole = true
		default:
			return fmt.Errorf("load %s role: %w", roleSlug, err)
		}
		if seedRole {
			name := roleSlug
			description := ""
			switch roleSlug {
			case SystemRoleAdmin:
				name = "Admin"
				description = "Administrator role"
			case SystemRoleMember:
				name = "Member"
				description = "Member role"
			}
			now := time.Now().UTC()
			if err := q.UpsertGlobalRole(ctx, repo.UpsertGlobalRoleParams{
				WorkosSlug:        roleSlug,
				WorkosName:        name,
				WorkosDescription: conv.ToPGTextEmpty(description),
				WorkosCreatedAt:   conv.ToPGTimestamptz(now),
				WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
				WorkosLastEventID: conv.ToPGTextEmpty(""),
			}); err != nil {
				return fmt.Errorf("seed %s role: %w", roleSlug, err)
			}
		}

		rp, err := loadRolePrincipals(ctx, dbtx, organizationID, roleSlug, "")
		if err != nil {
			return fmt.Errorf("resolve %s role principal: %w", roleSlug, err)
		}
		principalURNs, err := principalURNStrings(rp.MatchPrincipals)
		if err != nil {
			return fmt.Errorf("build %s role principals: %w", roleSlug, err)
		}
		existingGrants, err := q.GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
			OrganizationID: organizationID,
			PrincipalUrns:  principalURNs,
		})
		if err != nil {
			return fmt.Errorf("list %s grants: %w", roleSlug, err)
		}
		if len(existingGrants) > 0 {
			continue
		}

		rows, err := flattenRoleGrants(grants)
		if err != nil {
			return fmt.Errorf("build %s grants: %w", roleSlug, err)
		}
		if err := rp.insertGrantsIfAbsent(ctx, q, organizationID, rows); err != nil {
			return fmt.Errorf("seed %s grants: %w", roleSlug, err)
		}
	}
	return nil
}

// PatchRoleGrantsTx applies exact grant additions and removals for a role
// principal without treating omitted grants as deletes.
func PatchRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, orgID string, roleSlug string, rolePrincipalURN string, addGrants []*RoleGrant, removeGrants []*RoleGrant) ([]*ScopedGrant, error) {
	if orgID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	rp, err := loadRolePrincipals(ctx, dbtx, orgID, roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, err
	}

	q := repo.New(dbtx)
	removeRows, err := flattenRoleGrants(removeGrants)
	if err != nil {
		return nil, err
	}
	if err := rp.deleteGrants(ctx, q, orgID, removeRows); err != nil {
		return nil, err
	}

	addRows, err := flattenRoleGrants(addGrants)
	if err != nil {
		return nil, err
	}
	if err := rp.upsertGrants(ctx, q, orgID, addRows); err != nil {
		return nil, err
	}

	principalURNs, err := principalURNStrings(rp.MatchPrincipals)
	if err != nil {
		return nil, fmt.Errorf("build role principals: %w", err)
	}
	rows, err := q.GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
		OrganizationID: orgID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("list grants for role: %w", err)
	}

	return scopedGrantsFromGrantRows(rows)
}

type roleGrantRow struct {
	Scope       Scope
	Effect      PolicyEffect
	Selector    Selector
	SelectorRaw []byte
}

type roleGrantKey struct {
	scope    Scope
	effect   PolicyEffect
	selector string
}

func flattenRoleGrants(grants []*RoleGrant) ([]roleGrantRow, error) {
	rows := make([]roleGrantRow, 0, len(grants))
	seenGrants := make(map[roleGrantKey]struct{}, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}

		scope := Scope(grant.Scope)
		effect := conv.Default(grant.Effect, PolicyEffectAllow)
		if err := validatePolicyEffect(effect); err != nil {
			return nil, err
		}

		selectors := grant.Selectors
		// nil selectors = unrestricted wildcard access.
		// empty non-nil selectors = no rows.
		if selectors == nil {
			selectors = []Selector{NewSelector(scope, WildcardResource)}
		}

		for _, sel := range selectors {
			if err := ValidateSelector(scope, sel); err != nil {
				return nil, fmt.Errorf("invalid selector for scope %q: %w", grant.Scope, err)
			}

			selBytes, err := sel.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("marshal selector for scope %q: %w", grant.Scope, err)
			}
			grantKey := roleGrantKey{scope: scope, effect: effect, selector: string(selBytes)}
			if _, ok := seenGrants[grantKey]; ok {
				continue
			}
			seenGrants[grantKey] = struct{}{}
			rows = append(rows, roleGrantRow{
				Scope:       scope,
				Effect:      effect,
				Selector:    sel,
				SelectorRaw: selBytes,
			})
		}
	}

	return rows, nil
}

func validatePolicyEffect(effect PolicyEffect) error {
	switch conv.Default(effect, PolicyEffectAllow) {
	case PolicyEffectAllow, PolicyEffectDeny:
		return nil
	default:
		return fmt.Errorf("invalid policy effect %q", effect)
	}
}

func GrantsForRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, rolePrincipalURN string) ([]*ScopedGrant, error) {
	// TODO(AGE-1954): remove dual-read after legacy role:<slug> grants are backfilled.
	// During the role-principal migration, reads include both the canonical
	// role:<kind>:<uuid> principal and the legacy role:<slug> principal.
	rp, err := newRolePrincipals(roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build role principals").Log(ctx, logger)
	}
	principalURNs, err := principalURNStrings(rp.MatchPrincipals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build role principals").Log(ctx, logger)
	}

	rows, err := repo.New(db).GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
		OrganizationID: orgID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, logger)
	}

	scoped, err := scopedGrantsFromGrantRows(rows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unmarshal grant selector").Log(ctx, logger)
	}

	return scoped, nil
}

func scopedGrantsFromGrantRows(rows []repo.GetPrincipalGrantsRow) ([]*ScopedGrant, error) {
	grantRows := make([]Grant, 0, len(rows))
	for _, row := range rows {
		scope := Scope(row.Scope)
		selectors, err := SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, err
		}
		grantRows = append(grantRows, Grant{
			PrincipalUrn: row.PrincipalUrn.String(),
			Scope:        scope,
			Effect:       policyEffectFromText(row.Effect),
			Selector:     selectors,
		})
	}

	return GrantsToScopedGrants(grantRows), nil
}

// pgText converts a PolicyEffect to pgtype.Text for DB storage.
// Allow maps to NULL (backward compatible with existing rows).
// Deny maps to 'deny'.
func (e PolicyEffect) pgText() pgtype.Text {
	effect := conv.Default(e, PolicyEffectAllow)
	if effect == PolicyEffectAllow {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: string(effect), Valid: true}
}

// policyEffectFromText converts a nullable DB string to PolicyEffect.
// NULL or empty → allow.
func policyEffectFromText(s pgtype.Text) PolicyEffect {
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
func GrantsToScopedGrants(rows []Grant) []*ScopedGrant {
	grouped := groupGrantsByScopeEffect(rows)
	collapsed := collapseUnrestrictedSelectors(grouped)
	keys := sortedScopeEffectKeys(collapsed)
	return buildScopedGrants(keys, collapsed)
}

// groupGrantsByScopeEffect preserves selector rows while splitting allow and
// deny grants into independent output buckets for the same scope.
func groupGrantsByScopeEffect(rows []Grant) map[scopeEffectKey][]Selector {
	grouped := make(map[scopeEffectKey][]Selector)
	for _, row := range rows {
		key := scopeEffectKey{scope: string(row.Scope), effect: row.Effect}
		grouped[key] = append(grouped[key], row.Selector)
	}
	return grouped
}

// collapseUnrestrictedSelectors turns any unrestricted selector in a bucket
// into the API's nil-selector representation and drops narrower selectors that
// are redundant under that wildcard.
func collapseUnrestrictedSelectors(grouped map[scopeEffectKey][]Selector) map[scopeEffectKey]scopeAgg {
	collapsed := make(map[scopeEffectKey]scopeAgg, len(grouped))
	for key, selectors := range grouped {
		agg := scopeAgg{unrestricted: false, selectors: nil}
		for _, selector := range selectors {
			if !selector.IsRestricted() {
				agg.unrestricted = true
				agg.selectors = nil
				break
			}
			agg.selectors = append(agg.selectors, selector)
		}
		collapsed[key] = agg
	}
	return collapsed
}

// sortedScopeEffectKeys gives GrantsToScopedGrants stable output across map
// iteration order.
func sortedScopeEffectKeys(grouped map[scopeEffectKey]scopeAgg) []scopeEffectKey {
	keys := make([]scopeEffectKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b scopeEffectKey) int {
		if c := cmp.Compare(a.scope, b.scope); c != 0 {
			return c
		}
		return cmp.Compare(string(a.effect), string(b.effect))
	})
	return keys
}

// buildScopedGrants converts grouped selectors into the API shape and attaches
// the transitive sub-scopes for each scope.
func buildScopedGrants(keys []scopeEffectKey, grouped map[scopeEffectKey]scopeAgg) []*ScopedGrant {
	grants := make([]*ScopedGrant, 0, len(keys))
	for _, key := range keys {
		agg := grouped[key]
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
	if hasMatchingDenyGrant(grants, checks) {
		return nil, nil, true
	}

	allowGrant, allowCheck = matchingAllowGrant(grants, checks)
	if allowGrant == nil {
		return nil, nil, false
	}

	return allowGrant, allowCheck, false
}

func hasMatchingDenyGrant(grants []Grant, checks []Check) bool {
	for i := range grants {
		grant := &grants[i]
		if grant.Effect != PolicyEffectDeny {
			continue
		}

		for j := range checks {
			check := &checks[j]
			if grant.Scope != check.Scope {
				continue
			}

			// Deny only matches the original scope, not expanded parents.
			// Uses StrictMatches: every dimension in the deny selector must
			// be present in the check. This prevents a tool-scoped deny
			// from blocking a dimensionless server-level connect probe.
			if !check.expanded && grant.Selector.StrictMatches(check.selector()) {
				return true
			}
		}
	}

	return false
}

func matchingAllowGrant(grants []Grant, checks []Check) (*Grant, *Check) {
	for i := range grants {
		grant := &grants[i]
		if grant.Effect != PolicyEffectAllow {
			continue
		}

		for j := range checks {
			check := &checks[j]
			if grant.Scope != check.Scope {
				continue
			}

			if !check.matchesAllowSelector(grant.Selector) {
				continue
			}
			return grant, check
		}
	}

	return nil, nil
}

// allScopeGrants returns wildcard grants for every defined scope. Used to give
// superadmins (e.g. during org impersonation) unrestricted access.
func allScopeGrants() []Grant {
	grants := make([]Grant, 0, len(allScopes))
	for _, s := range allScopes {
		grants = append(grants, NewGrant(s, WildcardResource))
	}
	return grants
}

func roleGrantsForScopes(scopes []Scope) []*RoleGrant {
	grants := make([]*RoleGrant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, &RoleGrant{
			Scope:     string(scope),
			Effect:    PolicyEffectAllow,
			Selectors: nil,
		})
	}
	return grants
}
