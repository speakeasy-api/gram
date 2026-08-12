package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
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

type RoleGrant struct {
	Scope     string
	Selectors []Selector
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

// GrantsSatisfy reports whether the loaded grant set authorizes check.
func GrantsSatisfy(grants []Grant, check Check) bool {
	if err := validateInput(check); err != nil {
		return false
	}
	grant, _ := matchingGrant(grants, check.expand())
	return grant != nil
}

// SystemRoleGrants defines the canonical grant sets for the built-in system
// roles. These are seeded when an organization is provisioned. Existing grants
// for a built-in role are preserved, and custom roles are never touched.
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

	if err := SeedSystemRoleGrantsTx(ctx, tx, organizationID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit system role seed transaction: %w", err)
	}

	return nil
}

// SeedSystemRoleGrantsTx seeds the fixed grant sets for system roles using the
// caller's transaction. Use this when seeding must be atomic with other writes
// in an existing transaction (e.g. provisioning an org from a WorkOS webhook).
// Callers that only have a pool should use SeedSystemRoleGrants. Idempotent:
// roles that already hold grants are skipped.
func SeedSystemRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
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
	Selector    Selector
	SelectorRaw []byte
}

type roleGrantKey struct {
	scope    Scope
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
			grantKey := roleGrantKey{scope: scope, selector: string(selBytes)}
			if _, ok := seenGrants[grantKey]; ok {
				continue
			}
			seenGrants[grantKey] = struct{}{}
			rows = append(rows, roleGrantRow{
				Scope:       scope,
				Selector:    sel,
				SelectorRaw: selBytes,
			})
		}
	}

	return rows, nil
}

func GrantsForRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, rolePrincipalURN string) ([]*ScopedGrant, error) {
	// TODO(AGE-1954): remove dual-read after legacy role:<slug> grants are backfilled.
	// During the role-principal migration, reads include both the canonical
	// role:<kind>:<uuid> principal and the legacy role:<slug> principal.
	rp, err := newRolePrincipals(roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build role principals").LogError(ctx, logger)
	}
	principalURNs, err := principalURNStrings(rp.MatchPrincipals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build role principals").LogError(ctx, logger)
	}

	rows, err := repo.New(db).GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
		OrganizationID: orgID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").LogError(ctx, logger)
	}

	scoped, err := scopedGrantsFromGrantRows(rows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unmarshal grant selector").LogError(ctx, logger)
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
	grouped := groupGrantsByScope(rows)
	collapsed := collapseUnrestrictedSelectors(grouped)
	keys := sortedScopeKeys(collapsed)
	return buildScopedGrants(keys, collapsed)
}

func groupGrantsByScope(rows []Grant) map[string][]Selector {
	grouped := make(map[string][]Selector)
	for _, row := range rows {
		grouped[string(row.Scope)] = append(grouped[string(row.Scope)], row.Selector)
	}
	return grouped
}

// collapseUnrestrictedSelectors turns any unrestricted selector in a bucket
// into the API's nil-selector representation and drops narrower selectors that
// are redundant under that wildcard.
func collapseUnrestrictedSelectors(grouped map[string][]Selector) map[string]scopeAgg {
	collapsed := make(map[string]scopeAgg, len(grouped))
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

// sortedScopeKeys gives GrantsToScopedGrants stable output across map iteration.
func sortedScopeKeys(grouped map[string]scopeAgg) []string {
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// buildScopedGrants converts grouped selectors into the API shape and attaches
// the transitive sub-scopes for each scope.
func buildScopedGrants(keys []string, grouped map[string]scopeAgg) []*ScopedGrant {
	grants := make([]*ScopedGrant, 0, len(keys))
	for _, key := range keys {
		agg := grouped[key]
		subScopes := CalculateSubScopes(Scope(key))

		grant := &ScopedGrant{Scope: key, SubScopes: subScopes, Selectors: nil}
		if !agg.unrestricted {
			grant.Selectors = append([]Selector(nil), agg.selectors...)
		}
		grants = append(grants, grant)
	}

	return grants
}

type grantCheckEvaluation struct {
	Grant  *Grant
	Check  *Check
	Denied bool
}

func evaluateGrantCheck(grants []Grant, check Check) (grantCheckEvaluation, error) {
	grant, matchedCheck := matchingGrant(grants, check.expand())
	if grant == nil {
		return grantCheckEvaluation{Grant: nil, Check: nil, Denied: false}, nil
	}

	expression := expressionForCheck(check)
	if expression == nil {
		return grantCheckEvaluation{Grant: grant, Check: matchedCheck, Denied: false}, nil
	}

	result, err := expression.Evaluate(grants)
	if err != nil {
		return grantCheckEvaluation{}, fmt.Errorf("evaluate exclusion expression: %w", err)
	}
	if !result.Satisfied {
		return grantCheckEvaluation{Grant: nil, Check: nil, Denied: result.Reason == GrantExpressionReasonExclusionMatched}, nil
	}

	return grantCheckEvaluation{Grant: grant, Check: matchedCheck, Denied: false}, nil
}

func matchingGrant(grants []Grant, checks []Check) (*Grant, *Check) {
	for i := range grants {
		grant := &grants[i]
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

// allScopeGrants returns wildcard grants for every user-visible scope. Used to
// give platform admins (e.g. during org impersonation) unrestricted access without
// exposing internal blocklist storage scopes as standalone permissions.
func allScopeGrants() []Grant {
	grants := make([]Grant, 0, len(scopeVisibilityByScope))
	for s, visibility := range scopeVisibilityByScope {
		if visibility != scopeVisibilityUserVisible {
			continue
		}
		grants = append(grants, NewGrant(s, WildcardResource))
	}
	return grants
}

// DemoScopeGrants returns the fixed read-only grant set for sessions pointed
// at the shared demo organization. Deliberately excludes environment:read
// (secrets-adjacent) and every write scope.
func DemoScopeGrants() []Grant {
	scopes := []Scope{
		ScopeOrgRead,
		ScopeProjectRead,
		ScopeMCPRead,
		ScopeSkillRead,
		ScopeChatRead,
		ScopeRiskPolicyRead,
	}
	grants := make([]Grant, 0, len(scopes))
	for _, s := range scopes {
		grants = append(grants, NewGrant(s, WildcardResource))
	}
	return grants
}

func roleGrantsForScopes(scopes []Scope) []*RoleGrant {
	grants := make([]*RoleGrant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, &RoleGrant{
			Scope:     string(scope),
			Selectors: nil,
		})
	}
	return grants
}
