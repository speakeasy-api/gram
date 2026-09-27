package remotesessions

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Guards run in the mutation transaction. Exact tier-qualified row locks
// revalidate ownership after any wait; project ownership comes from projects,
// since legacy project-owned rows can have a NULL organization_id.
func guardEMABindingsForClient(ctx context.Context, r *repo.Queries, organizationID string, projectID, clientID uuid.UUID) error {
	organizationID, err := lifecycleOrganization(ctx, r, organizationID, projectID)
	if err != nil {
		return err
	}
	if _, err := r.LockEMAClientForLifecycle(ctx, repo.LockEMAClientForLifecycleParams{ID: clientID, ProjectID: projectID, OrganizationID: organizationID}); err != nil {
		return lifecycleLockError(err)
	}
	count, err := r.CountActiveEMABindingsForClient(ctx, repo.CountActiveEMABindingsForClientParams{ClientID: conv.ToNullUUID(clientID), OrganizationID: organizationID, ProjectID: projectID})
	return requireNoEMABindings(count, err)
}

// Detach callers hold the user issuer and client locks in preparation order.
// Organization detach removes the shared association, so it checks all projects
// in that organization; project detach checks only the authorized project.
func guardEMABindingsForClientUserIssuer(ctx context.Context, r *repo.Queries, organizationID string, projectID, clientID, userIssuerID uuid.UUID) error {
	count, err := r.CountActiveEMABindingsForClientUserIssuer(ctx, repo.CountActiveEMABindingsForClientUserIssuerParams{
		ClientID: conv.ToNullUUID(clientID), UserSessionIssuerID: userIssuerID,
		OrganizationID: organizationID, ProjectID: projectID,
	})
	return requireNoEMABindings(count, err)
}
