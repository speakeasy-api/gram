package remotesessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

func guardEMABindingsForIssuer(ctx context.Context, r *repo.Queries, organizationID string, projectID, issuerID uuid.UUID) error {
	organizationID, err := lifecycleOrganization(ctx, r, organizationID, projectID)
	if err != nil {
		return err
	}
	if _, err := r.LockEMAIssuerForLifecycle(ctx, repo.LockEMAIssuerForLifecycleParams{ID: issuerID, ProjectID: projectID, OrganizationID: organizationID}); err != nil {
		return lifecycleLockError(err)
	}
	count, err := r.CountActiveEMABindingsForIssuer(ctx, repo.CountActiveEMABindingsForIssuerParams{IssuerID: issuerID, OrganizationID: organizationID, ProjectID: projectID})
	return requireNoEMABindings(count, err)
}

// Rotation and migration can start from an already-authorized legacy row.
// Resolve its missing organization without making the project a wildcard.
func lifecycleOrganization(ctx context.Context, r *repo.Queries, organizationID string, projectID uuid.UUID) (string, error) {
	if projectID != uuid.Nil && organizationID == "" {
		organizationID, err := r.GetEMAProjectOrganization(ctx, projectID)
		if err != nil {
			return "", lifecycleLockError(err)
		}
		return organizationID, nil
	}
	return organizationID, nil
}

func lifecycleLockError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "identity-chaining configuration no longer belongs to the requested scope")
	}
	return oops.E(oops.CodeUnexpected, err, "lock identity-chaining configuration")
}

func requireNoEMABindings(count int64, err error) error {
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check identity-chaining bindings")
	}
	if count > 0 {
		return oops.E(oops.CodeConflict, nil, "active identity-chaining bindings reference this configuration; explicitly unlink them before changing or deleting it")
	}
	return nil
}

// Discovery may replace evidence on an active binding, including capability
// removal. Endpoint changes instead reconfigure the selected authorization
// server and require unlinking. Call after the refresh snapshot's row lock;
// never hold that lock during discovery HTTP.
func guardEMAEndpointRefresh(ctx context.Context, q *repo.Queries, existing repo.RemoteSessionIssuer, next repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams) error {
	if existing.AuthorizationEndpoint.String == next.AuthorizationEndpoint &&
		existing.TokenEndpoint.String == next.TokenEndpoint &&
		existing.RevocationEndpoint.String == next.RevocationEndpoint &&
		existing.RegistrationEndpoint.String == next.RegistrationEndpoint &&
		existing.JwksUri.String == next.JwksUri &&
		existing.UserinfoEndpoint.String == next.UserinfoEndpoint &&
		existing.IntrospectionEndpoint.String == next.IntrospectionEndpoint {
		return nil
	}
	return guardEMABindingsForIssuer(ctx, q, existing.OrganizationID.String, existing.ProjectID.UUID, existing.ID)
}
