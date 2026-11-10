package assistantidentity

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// CheckActor reports ErrActorIneligible unless user is an active member of org
// whose own grants can read project. The grants of whatever transport carried
// the request never count.
func CheckActor(ctx context.Context, db orgrepo.DBTX, engine *authz.Engine, org string, project uuid.UUID, user string) error {
	active, err := orgrepo.New(db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: org, UserID: user})
	if err != nil {
		return fmt.Errorf("check actor membership: %w", err)
	}
	if !active {
		return fmt.Errorf("%w: not an active organization member", ErrActorIneligible)
	}
	principals, err := authz.ResolveUserPrincipals(ctx, db, org, user)
	if err != nil {
		return fmt.Errorf("resolve actor principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, db, org, principals)
	if err != nil {
		return fmt.Errorf("load actor grants: %w", err)
	}
	check := authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: project.String(), Dimensions: nil}
	if err := engine.EvaluateLoadedGrants(ctx, grants, check); err != nil {
		return fmt.Errorf("%w: cannot read the project: %w", ErrActorIneligible, err)
	}
	return nil
}

// canDelegateInference reads a user's own current authority, not a request's
// transport grants. Missing authority leaves a new agent without inference.
func canDelegateInference(ctx context.Context, db orgrepo.DBTX, org string, project uuid.UUID, user string) (bool, error) {
	active, err := orgrepo.New(db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: org, UserID: user})
	if err != nil {
		return false, fmt.Errorf("check inference owner membership: %w", err)
	}
	if !active {
		return false, nil
	}
	principals, err := authz.ResolveUserPrincipals(ctx, db, org, user)
	if err != nil {
		return false, fmt.Errorf("resolve inference owner: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, db, org, principals)
	if err != nil {
		return false, fmt.Errorf("load inference owner grants: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(grants, authz.InferenceExecuteCheck(project.String()))
	if err != nil {
		return false, fmt.Errorf("check inference delegation: %w", err)
	}
	return allowed, nil
}
