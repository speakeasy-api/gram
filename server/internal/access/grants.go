package access

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type RoleGrant struct {
	Scope     string
	Resources []string
}

const WildcardResource = "*"

type Grant struct {
	Scope    Scope
	Resource string
}

type Grants struct {
	rows []Grant
}

func (g *Grants) hasAccess(scope Scope, resourceID string) bool {
	if g == nil {
		return false
	}

	for _, row := range g.rows {
		if row.Scope != scope {
			continue
		}
		if row.Resource == WildcardResource || row.Resource == resourceID {
			return true
		}
	}

	return false
}

func syncGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string, grants []*RoleGrant) error {
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
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Resource:       WildcardResource,
			}); err != nil {
				return fmt.Errorf("upsert unrestricted grant %q for role %q: %w", grant.Scope, roleSlug, err)
			}

			continue
		}

		for _, resource := range grant.Resources {
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Resource:       resource,
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

func grantsForRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, orgID string, roleSlug string) ([]*gen.RoleGrant, error) {
	rows, err := repo.New(db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug).String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, logger)
	}

	type scopeAgg struct {
		unrestricted bool
		resources    []string
	}
	byScope := make(map[string]*scopeAgg)
	for _, row := range rows {
		agg, ok := byScope[row.Scope]
		if !ok {
			agg = &scopeAgg{unrestricted: false, resources: nil}
			byScope[row.Scope] = agg
		}
		if row.Resource == WildcardResource {
			agg.unrestricted = true
			agg.resources = nil
			continue
		}
		if !agg.unrestricted {
			agg.resources = append(agg.resources, row.Resource)
		}
	}

	grants := make([]*gen.RoleGrant, 0, len(byScope))
	for scope, agg := range byScope {
		grant := &gen.RoleGrant{Scope: scope, Resources: nil}
		if agg.unrestricted {
			grant.Resources = nil
		} else {
			grant.Resources = append([]string(nil), agg.resources...)
		}
		grants = append(grants, grant)
	}

	return grants, nil
}
