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
	"github.com/speakeasy-api/gram/server/internal/urn"
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

var allScopes = []Scope{
	ScopeOrgRead,
	ScopeOrgAdmin,
	ScopeProjectRead,
	ScopeProjectWrite,
	ScopeMCPRead,
	ScopeMCPWrite,
	ScopeMCPConnect,
	ScopeEnvironmentRead,
	ScopeEnvironmentWrite,
	ScopeRiskPolicyEvaluate,
}

var adminScopes = []Scope{
	ScopeOrgRead,
	ScopeOrgAdmin,
	ScopeProjectRead,
	ScopeProjectWrite,
	ScopeMCPRead,
	ScopeMCPWrite,
	ScopeMCPConnect,
	ScopeEnvironmentRead,
	ScopeEnvironmentWrite,
}

var memberScopes = []Scope{
	ScopeOrgRead,
	ScopeProjectRead,
	ScopeMCPRead,
	ScopeMCPConnect,
	ScopeEnvironmentRead,
}

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

// SystemRoleGrants defines the canonical grant sets for the built-in system
// roles. These are seeded when RBAC is enabled and replace any existing grants
// for these roles (idempotent, won't clobber custom roles).
var SystemRoleGrants = map[string][]*RoleGrant{
	SystemRoleAdmin:  roleGrantsForScopes(adminScopes),
	SystemRoleMember: roleGrantsForScopes(memberScopes),
}

// SeedSystemRoleGrants upserts the fixed grant sets for all system roles.
func SeedSystemRoleGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	for roleSlug, grants := range SystemRoleGrants {
		existingRole, err := repo.New(db).GetGlobalRoleBySlug(ctx, roleSlug)
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
			if err := repo.New(db).UpsertGlobalRole(ctx, repo.UpsertGlobalRoleParams{
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
		if err := ReplaceRoleGrants(ctx, logger, db, organizationID, roleSlug, "", grants); err != nil {
			return fmt.Errorf("seed %s grants: %w", roleSlug, err)
		}
	}
	return nil
}

func ReplaceRoleGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, rolePrincipalURN string, grants []*RoleGrant) error {
	if orgID == "" {
		return fmt.Errorf("organization id is required")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin grant sync transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if _, err := ReplaceRoleGrantsTx(ctx, tx, orgID, roleSlug, rolePrincipalURN, grants); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit grant sync transaction: %w", err)
	}

	return nil
}

func ReplaceRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, orgID string, roleSlug string, rolePrincipalURN string, grants []*RoleGrant) ([]*ScopedGrant, error) {
	if orgID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	roleIdentity, err := resolveRoleGrantIdentity(ctx, dbtx, orgID, roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, err
	}

	q := repo.New(dbtx)
	// During the role-principal migration, replace grants for both the new
	// role:<kind>:<uuid> principal and the legacy role:<slug> principal. New
	// writes below only insert the canonical URN form.
	if err := deleteRoleGrantPrincipals(ctx, q, orgID, roleIdentity); err != nil {
		return nil, err
	}

	rows, err := roleGrantRowsFromRoleGrants(grants)
	if err != nil {
		return nil, err
	}
	if err := upsertRoleGrantRows(ctx, q, orgID, roleIdentity, rows); err != nil {
		return nil, err
	}

	return GrantsToScopedGrants(grantsFromRows(roleIdentity.WritePrincipal, rows)), nil
}

// PatchRoleGrantsTx applies exact grant additions and removals for a role
// principal without treating omitted grants as deletes.
func PatchRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, orgID string, roleSlug string, rolePrincipalURN string, addGrants []*RoleGrant, removeGrants []*RoleGrant) ([]*ScopedGrant, error) {
	if orgID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	roleIdentity, err := resolveRoleGrantIdentity(ctx, dbtx, orgID, roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, err
	}

	q := repo.New(dbtx)
	removeRows, err := roleGrantRowsFromRoleGrants(removeGrants)
	if err != nil {
		return nil, err
	}
	if err := deleteRoleGrantRows(ctx, q, orgID, roleIdentity, removeRows); err != nil {
		return nil, err
	}

	addRows, err := roleGrantRowsFromRoleGrants(addGrants)
	if err != nil {
		return nil, err
	}
	if err := upsertRoleGrantRows(ctx, q, orgID, roleIdentity, addRows); err != nil {
		return nil, err
	}

	principalURNs, err := parsePrincipalURNs(roleIdentity.MatchPrincipals)
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

func roleGrantRowsFromRoleGrants(grants []*RoleGrant) ([]roleGrantRow, error) {
	rows := make([]roleGrantRow, 0, len(grants))
	seenGrants := make(map[roleGrantKey]struct{}, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}

		scope := Scope(grant.Scope)
		effect := conv.Default(grant.Effect, PolicyEffectAllow)

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

func upsertRoleGrantRows(ctx context.Context, q *repo.Queries, orgID string, roleIdentity roleGrantIdentity, rows []roleGrantRow) error {
	for _, row := range rows {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: orgID,
			PrincipalUrn:   roleIdentity.WritePrincipal,
			Scope:          string(row.Scope),
			Effect:         effectToPgtype(row.Effect),
			Selectors:      row.SelectorRaw,
		}); err != nil {
			return fmt.Errorf("upsert grant %q for role %q: %w", row.Scope, roleIdentity.Slug, err)
		}
	}

	return nil
}

func deleteRoleGrantRows(ctx context.Context, q *repo.Queries, orgID string, roleIdentity roleGrantIdentity, rows []roleGrantRow) error {
	for _, principal := range roleIdentity.MatchPrincipals {
		for _, row := range rows {
			if _, err := q.DeletePrincipalGrantByIdentity(ctx, repo.DeletePrincipalGrantByIdentityParams{
				OrganizationID: orgID,
				PrincipalUrn:   principal,
				Scope:          string(row.Scope),
				Effect:         string(row.Effect),
				Selectors:      row.SelectorRaw,
			}); err != nil {
				return fmt.Errorf("delete grant %q for role %q: %w", row.Scope, roleIdentity.Slug, err)
			}
		}
	}

	return nil
}

func grantsFromRows(principalURN urn.Principal, rows []roleGrantRow) []Grant {
	grants := make([]Grant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, Grant{
			PrincipalUrn: principalURN.String(),
			Scope:        row.Scope,
			Effect:       row.Effect,
			Selector:     row.Selector,
		})
	}

	return grants
}

func GrantsForRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, rolePrincipalURN string) ([]*ScopedGrant, error) {
	principals, err := RolePrincipals(roleSlug, rolePrincipalURN)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build role principals").Log(ctx, logger)
	}
	principalURNs, err := parsePrincipalURNs(principals)
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
			Effect:       effectFromNullable(row.Effect),
			Selector:     selectors,
		})
	}

	return GrantsToScopedGrants(grantRows), nil
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
func GrantsToScopedGrants(rows []Grant) []*ScopedGrant {
	grouped := groupGrantsByScopeEffect(rows)
	collapsed := collapseUnrestrictedSelectors(grouped)
	keys := sortedScopeEffectKeys(collapsed)
	return buildScopedGrants(keys, collapsed)
}

func groupGrantsByScopeEffect(rows []Grant) map[scopeEffectKey][]Selector {
	grouped := make(map[scopeEffectKey][]Selector)
	for _, row := range rows {
		key := scopeEffectKey{scope: string(row.Scope), effect: row.Effect}
		grouped[key] = append(grouped[key], row.Selector)
	}
	return grouped
}

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
				return nil, nil, true
			}
		}
	}

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

			// Allow: permissive matching (skip missing dimensions).
			if !grant.Selector.Matches(check.selector()) {
				continue
			}
			return grant, check, false
		}
	}

	return nil, nil, false
}

// findMatchingGrant compares a list of grants against a list of checks and returns
// the first grant / check tuple that is satisfied. Deny grants block the match.
func findMatchingGrant(grants []Grant, checks []Check) (*Grant, *Check) {
	allow, check, _ := evaluateGrants(grants, checks)
	return allow, check
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
