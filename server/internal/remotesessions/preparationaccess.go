package remotesessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) preparationTenant(ctx context.Context, write bool) (uuid.UUID, string, error) {
	a, ok := contextvalues.GetAuthContext(ctx)
	if !ok || a == nil || a.ProjectID == nil || *a.ProjectID == uuid.Nil || a.ActiveOrganizationID == "" {
		return uuid.Nil, "", oops.C(oops.CodeUnauthorized)
	}
	scope := authz.ScopeProjectRead
	if write {
		scope = authz.ScopeProjectWrite
	}
	if err := s.authz.Require(ctx, authz.Check{ResourceKind: "", Dimensions: nil, Scope: scope, ResourceID: a.ProjectID.String()}); err != nil {
		return uuid.Nil, "", err
	}
	return *a.ProjectID, a.ActiveOrganizationID, nil
}

// recheckPreparationTenant preserves the tenant selected before waiting and
// re-evaluates the request's required scope before starting a mutation.
func (s *Service) recheckPreparationTenant(ctx context.Context, project uuid.UUID, org string, write bool) error {
	currentProject, currentOrg, err := s.preparationTenant(ctx, write)
	if err != nil {
		return err
	}
	if currentProject != project || currentOrg != org {
		return oops.C(oops.CodeForbidden)
	}
	return nil
}

func preparationLookupError(err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "%s", message)
	}
	return oops.E(oops.CodeUnexpected, err, "failed to lock preparation resource")
}
