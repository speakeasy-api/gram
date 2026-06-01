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
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin resource grant transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := ReplaceGrantsForResourceTx(ctx, tx, organizationID, scope, resourceID, principals); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit resource grant transaction: %w", err)
	}

	return nil
}

// ReplaceGrantsForResourceTx replaces allow grants using the caller's transaction.
func ReplaceGrantsForResourceTx(ctx context.Context, db repo.DBTX, organizationID string, scope Scope, resourceID string, principals []urn.Principal) error {
	if organizationID == "" {
		return fmt.Errorf("organization id is required")
	}
	if scope == "" {
		return fmt.Errorf("scope is required")
	}
	if resourceID == "" {
		return fmt.Errorf("resource id is required")
	}

	uniquePrincipals := make([]urn.Principal, 0, len(principals))
	seen := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		if _, err := principal.Value(); err != nil {
			return fmt.Errorf("invalid grant principal: %w", err)
		}
		if _, ok := seen[principal.String()]; ok {
			continue
		}
		seen[principal.String()] = struct{}{}
		uniquePrincipals = append(uniquePrincipals, principal)
	}

	resourceKind := ResourceKindForScope(scope)
	if resourceKind == WildcardResource {
		return fmt.Errorf("scope %q does not map to a resource kind", scope)
	}

	selector := NewSelector(scope, resourceID)
	if err := ValidateSelector(scope, selector); err != nil {
		return err
	}
	selectorBytes, err := selector.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal grant selector: %w", err)
	}

	q := repo.New(db)
	if _, err := q.DeletePrincipalGrantsByResource(ctx, repo.DeletePrincipalGrantsByResourceParams{
		OrganizationID: organizationID,
		Scope:          string(scope),
		ResourceKind:   resourceKind,
		ResourceID:     resourceID,
	}); err != nil {
		return fmt.Errorf("delete resource grants: %w", err)
	}

	for _, principal := range uniquePrincipals {
		if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: organizationID,
			PrincipalUrn:   principal,
			Scope:          string(scope),
			Effect:         effectToPgtype(PolicyEffectAllow),
			Selectors:      selectorBytes,
		}); err != nil {
			return fmt.Errorf("upsert resource grant: %w", err)
		}
	}

	return nil
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
			Effect:       effectFromNullable(row.Effect),
			Selector:     selector,
		})
	}

	return grants, nil
}
