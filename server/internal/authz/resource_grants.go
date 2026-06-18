package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Resource identifies one resource-scoped permission.
type Resource struct {
	OrganizationID string
	Scope          Scope
	ResourceID     string
}

func (r Resource) Validate() error {
	if r.OrganizationID == "" {
		return fmt.Errorf("organization id is required")
	}
	if r.Scope == "" {
		return fmt.Errorf("scope is required")
	}
	if r.ResourceID == "" {
		return fmt.Errorf("resource id is required")
	}
	if ResourceKindForScope(r.Scope) == WildcardResource {
		return fmt.Errorf("scope %q does not map to a resource kind", r.Scope)
	}

	return nil
}

func (r Resource) Kind() string {
	return ResourceKindForScope(r.Scope)
}

// ResourceGrant describes grant changes for one resource-scoped permission and
// one or more principals.
type ResourceGrant struct {
	Resource
	Effect     PolicyEffect
	Principals []urn.Principal
	Selector   Selector
}

func (r ResourceGrant) Validate() error {
	if err := r.Resource.Validate(); err != nil {
		return err
	}
	if err := validatePolicyEffect(r.Effect); err != nil {
		return err
	}
	for _, principal := range r.Principals {
		if _, err := principal.Value(); err != nil {
			return fmt.Errorf("invalid grant principal: %w", err)
		}
	}
	if r.Selector == nil {
		return nil
	}
	if r.Selector.ResourceID() != r.ResourceID {
		return fmt.Errorf("selector resource_id %q does not match resource id %q", r.Selector.ResourceID(), r.ResourceID)
	}
	if err := ValidateSelector(r.Scope, r.Selector); err != nil {
		return err
	}

	return nil
}

func (r ResourceGrant) ValidatePrincipals() error {
	if len(r.Principals) == 0 {
		return fmt.Errorf("at least one principal is required")
	}

	return nil
}

// ValidateAudience checks invariants for full-audience replacement writes.
// A grant target can apply to either user:all or a narrowed set of user/role
// principals, but combining both would store a redundant and ambiguous audience.
func (r ResourceGrant) ValidateAudience() error {
	principals := r.uniquePrincipals()
	hasAllUsers := false
	for _, principal := range principals {
		hasAllUsers = hasAllUsers || isAllUsersPrincipal(principal)
	}
	if hasAllUsers && len(principals) > 1 {
		return fmt.Errorf("user:all cannot be combined with narrower principals")
	}

	return nil
}

func (r ResourceGrant) selector() Selector {
	if r.Selector != nil {
		return r.Selector
	}
	return NewSelector(r.Scope, r.ResourceID)
}

func (r ResourceGrant) uniquePrincipals() []urn.Principal {
	principals := make([]urn.Principal, 0, len(r.Principals))
	seen := make(map[string]struct{}, len(r.Principals))
	for _, principal := range r.Principals {
		key := principal.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		principals = append(principals, principal)
	}
	return principals
}

// ReplaceGrantAudience replaces the full audience for one exact grant target.
// This is intentionally distinct from PatchPrincipalGrants: callers use this
// when they know the complete desired audience, such as switching from
// user:all to a narrowed user/role set.
func ReplaceGrantAudience(ctx context.Context, db repo.DBTX, resource ResourceGrant) error {
	if err := resource.Validate(); err != nil {
		return err
	}

	if err := resource.ValidateAudience(); err != nil {
		return err
	}

	selector := resource.selector()
	selectorBytes, err := selector.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal grant selector: %w", err)
	}

	effect := conv.Default(resource.Effect, PolicyEffectAllow)
	q := repo.New(db)
	if _, err := q.DeletePrincipalGrantsByTarget(ctx, repo.DeletePrincipalGrantsByTargetParams{
		OrganizationID: resource.OrganizationID,
		Scope:          string(resource.Scope),
		Effect:         string(effect),
		Selectors:      selectorBytes,
	}); err != nil {
		return fmt.Errorf("delete grant audience: %w", err)
	}

	for _, principal := range resource.uniquePrincipals() {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: resource.OrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(resource.Scope),
			Effect:         effect.pgText(),
			Selectors:      selectorBytes,
		}); err != nil {
			return fmt.Errorf("upsert grant audience principal: %w", err)
		}
	}

	return nil
}

// Replace replaces allow grants for one resource-scoped permission.
func ReplaceGrantsForResource(ctx context.Context, db repo.DBTX, resource ResourceGrant) error {
	if err := resource.Validate(); err != nil {
		return err
	}
	selector := resource.selector()
	principals := resource.uniquePrincipals()

	selectorBytes, err := selector.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal grant selector: %w", err)
	}

	q := repo.New(db)
	if _, err := q.DeletePrincipalGrantsByResource(ctx, repo.DeletePrincipalGrantsByResourceParams{
		OrganizationID: resource.OrganizationID,
		Scope:          string(resource.Scope),
		ResourceKind:   resource.Kind(),
		ResourceID:     resource.ResourceID,
	}); err != nil {
		return fmt.Errorf("delete resource grants: %w", err)
	}

	for _, principal := range principals {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: resource.OrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(resource.Scope),
			Effect:         PolicyEffectAllow.pgText(),
			Selectors:      selectorBytes,
		}); err != nil {
			return fmt.Errorf("upsert resource grant: %w", err)
		}
	}

	return nil
}

// Grant adds one allow grant per principal without replacing other principals'
// grants for the same resource.
func GrantResourceToPrincipals(ctx context.Context, db repo.DBTX, resource ResourceGrant) error {
	if err := resource.Validate(); err != nil {
		return err
	}
	if err := resource.ValidatePrincipals(); err != nil {
		return err
	}
	grant := &RoleGrant{
		Scope:     string(resource.Scope),
		Effect:    conv.Default(resource.Effect, PolicyEffectAllow),
		Selectors: []Selector{resource.selector()},
	}

	for _, principal := range resource.uniquePrincipals() {
		if err := PatchPrincipalGrants(ctx, db, resource.OrganizationID, principal, []*RoleGrant{grant}, nil); err != nil {
			return err
		}
	}

	return nil
}

// Revoke removes one exact allow grant per principal without replacing other
// principals' grants for the same resource.
func RevokeResourceFromPrincipals(ctx context.Context, db repo.DBTX, resource ResourceGrant) error {
	if err := resource.Validate(); err != nil {
		return err
	}
	if err := resource.ValidatePrincipals(); err != nil {
		return err
	}
	grant := &RoleGrant{
		Scope:     string(resource.Scope),
		Effect:    conv.Default(resource.Effect, PolicyEffectAllow),
		Selectors: []Selector{resource.selector()},
	}

	for _, principal := range resource.uniquePrincipals() {
		if err := PatchPrincipalGrants(ctx, db, resource.OrganizationID, principal, nil, []*RoleGrant{grant}); err != nil {
			return err
		}
	}

	return nil
}

// ListGrantsForResource loads grants for one resource-scoped permission.
func ListGrantsForResource(ctx context.Context, db repo.DBTX, resource Resource) ([]Grant, error) {
	if err := resource.Validate(); err != nil {
		return nil, err
	}

	rows, err := repo.New(db).ListPrincipalGrantsByResource(ctx, repo.ListPrincipalGrantsByResourceParams{
		OrganizationID: resource.OrganizationID,
		Scope:          string(resource.Scope),
		ResourceKind:   resource.Kind(),
		ResourceID:     resource.ResourceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list resource grants: %w", err)
	}

	grants := make([]Grant, 0, len(rows))
	for _, row := range rows {
		selector, err := SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, err
		}
		grants = append(grants, Grant{
			PrincipalUrn: row.PrincipalUrn.String(),
			Scope:        NormalizeScope(Scope(row.Scope)),
			Effect:       policyEffectFromText(row.Effect),
			Selector:     selector,
		})
	}

	return grants, nil
}
