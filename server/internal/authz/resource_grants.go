package authz

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ReplaceGrantsForResource replaces allow grants for one resource-scoped permission.
func ReplaceGrantsForResource(ctx context.Context, db *pgxpool.Pool, organizationID string, scope Scope, resourceID string, principals []urn.Principal) error {
	replacement, err := newResourceGrantReplacement(organizationID, scope, resourceID, principals)
	if err != nil {
		return err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin resource grant transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := replaceGrantsForResourceTx(ctx, tx, replacement); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit resource grant transaction: %w", err)
	}

	return nil
}

// ReplaceGrantsForResourceTx replaces allow grants using the caller's transaction.
func ReplaceGrantsForResourceTx(ctx context.Context, db repo.DBTX, organizationID string, scope Scope, resourceID string, principals []urn.Principal) error {
	replacement, err := newResourceGrantReplacement(organizationID, scope, resourceID, principals)
	if err != nil {
		return err
	}

	return replaceGrantsForResourceTx(ctx, db, replacement)
}

type resourceGrantReplacement struct {
	OrganizationID   string
	Scope            Scope
	ResourceID       string
	ResourceKind     string
	SelectorBytes    []byte
	UniquePrincipals []urn.Principal
}

func newResourceGrantReplacement(organizationID string, scope Scope, resourceID string, principals []urn.Principal) (resourceGrantReplacement, error) {
	if organizationID == "" {
		return resourceGrantReplacement{}, fmt.Errorf("organization id is required")
	}
	if scope == "" {
		return resourceGrantReplacement{}, fmt.Errorf("scope is required")
	}
	if resourceID == "" {
		return resourceGrantReplacement{}, fmt.Errorf("resource id is required")
	}

	uniquePrincipals := make([]urn.Principal, 0, len(principals))
	seen := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		if _, err := principal.Value(); err != nil {
			return resourceGrantReplacement{}, fmt.Errorf("invalid grant principal: %w", err)
		}
		if _, ok := seen[principal.String()]; ok {
			continue
		}
		seen[principal.String()] = struct{}{}
		uniquePrincipals = append(uniquePrincipals, principal)
	}

	resourceKind := ResourceKindForScope(scope)
	if resourceKind == WildcardResource {
		return resourceGrantReplacement{}, fmt.Errorf("scope %q does not map to a resource kind", scope)
	}

	selector := NewSelector(scope, resourceID)
	if err := ValidateSelector(scope, selector); err != nil {
		return resourceGrantReplacement{}, err
	}
	selectorBytes, err := selector.MarshalJSON()
	if err != nil {
		return resourceGrantReplacement{}, fmt.Errorf("marshal grant selector: %w", err)
	}

	return resourceGrantReplacement{
		OrganizationID:   organizationID,
		Scope:            scope,
		ResourceID:       resourceID,
		ResourceKind:     resourceKind,
		SelectorBytes:    selectorBytes,
		UniquePrincipals: uniquePrincipals,
	}, nil
}

func replaceGrantsForResourceTx(ctx context.Context, db repo.DBTX, replacement resourceGrantReplacement) error {
	q := repo.New(db)
	if _, err := q.DeletePrincipalGrantsByResource(ctx, repo.DeletePrincipalGrantsByResourceParams{
		OrganizationID: replacement.OrganizationID,
		Scope:          string(replacement.Scope),
		ResourceKind:   replacement.ResourceKind,
		ResourceID:     replacement.ResourceID,
	}); err != nil {
		return fmt.Errorf("delete resource grants: %w", err)
	}

	for _, principal := range replacement.UniquePrincipals {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: replacement.OrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(replacement.Scope),
			Effect:         PolicyEffectAllow.pgText(),
			Selectors:      replacement.SelectorBytes,
		}); err != nil {
			return fmt.Errorf("upsert resource grant: %w", err)
		}
	}

	return nil
}

// GrantResourceToPrincipalTx adds one allow grant for a principal without
// replacing other grants for the same resource.
func GrantResourceToPrincipalTx(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal, scope Scope, resourceID string) error {
	if resourceID == "" {
		return fmt.Errorf("resource id is required")
	}
	return GrantResourceToPrincipalWithSelectorTx(ctx, db, organizationID, principal, scope, NewSelector(scope, resourceID))
}

// GrantResourceToPrincipalWithSelectorTx adds one allow grant for a principal
// with an explicit selector, such as a risk_policy:bypass narrowed by server_url.
func GrantResourceToPrincipalWithSelectorTx(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal, scope Scope, selector Selector) error {
	return PatchPrincipalGrantsTx(ctx, db, organizationID, principal, []*RoleGrant{
		{
			Scope:     string(scope),
			Effect:    PolicyEffectAllow,
			Selectors: []Selector{selector},
		},
	}, nil)
}

// RevokeResourceFromPrincipalTx removes one allow grant for a principal without
// replacing other grants for the same resource.
func RevokeResourceFromPrincipalTx(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal, scope Scope, resourceID string) error {
	if resourceID == "" {
		return fmt.Errorf("resource id is required")
	}
	return RevokeResourceFromPrincipalWithSelectorTx(ctx, db, organizationID, principal, scope, NewSelector(scope, resourceID))
}

// RevokeResourceFromPrincipalWithSelectorTx removes one exact allow grant for a
// principal and selector.
func RevokeResourceFromPrincipalWithSelectorTx(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal, scope Scope, selector Selector) error {
	return PatchPrincipalGrantsTx(ctx, db, organizationID, principal, nil, []*RoleGrant{
		{
			Scope:     string(scope),
			Effect:    PolicyEffectAllow,
			Selectors: []Selector{selector},
		},
	})
}

// ListGrantsForResource loads grants for one resource-scoped permission.
func ListGrantsForResource(ctx context.Context, db repo.DBTX, organizationID string, scope Scope, resourceID string) ([]Grant, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}
	if scope == "" {
		return nil, fmt.Errorf("scope is required")
	}
	if resourceID == "" {
		return nil, fmt.Errorf("resource id is required")
	}

	resourceKind := ResourceKindForScope(scope)
	if resourceKind == WildcardResource {
		return nil, fmt.Errorf("scope %q does not map to a resource kind", scope)
	}

	rows, err := repo.New(db).ListPrincipalGrantsByResource(ctx, repo.ListPrincipalGrantsByResourceParams{
		OrganizationID: organizationID,
		Scope:          string(scope),
		ResourceKind:   resourceKind,
		ResourceID:     resourceID,
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
			Scope:        Scope(row.Scope),
			Effect:       policyEffectFromText(row.Effect),
			Selector:     selector,
		})
	}

	return grants, nil
}
