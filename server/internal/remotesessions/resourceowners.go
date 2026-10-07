package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// ResourceOwners reports, for every client bound to a login's user session
// issuer, whether a login through it is qualified to upstream's protected
// resource: the same decision login and the consent card make, for surfaces
// that hold only a database handle. A client missing from the map does not
// own the resource.
func ResourceOwners(ctx context.Context, db remotesessions_repo.DBTX, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, upstream string) (map[uuid.UUID]bool, error) {
	clientIDs, err := remotesessions_repo.New(db).ListRemoteSessionClientIDsForUserSessionIssuer(ctx, remotesessions_repo.ListRemoteSessionClientIDsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           projectID,
		OrganizationID:      organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list clients bound to user session issuer: %w", err)
	}
	attachments, err := attachmentsForClients(ctx, db, organizationID, clientIDs)
	if err != nil {
		return nil, err
	}
	owners := make(map[uuid.UUID]bool, len(clientIDs))
	for _, id := range clientIDs {
		resource := resourceAmongSiblings(attachments, id, upstream)
		owners[id] = resource != "" && sameUpstream(resource, upstream)
	}
	return owners, nil
}
