package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// ResourceOwnerQuery names one remote-backed server whose resource ownership
// is asked: the login's user session issuer, its project, and its upstream.
type ResourceOwnerQuery struct {
	ServerID            uuid.UUID
	ProjectID           uuid.UUID
	UserSessionIssuerID uuid.UUID
	Upstream            string
}

// ResourceOwners reports, for every client bound to a login's user session
// issuer, whether a login through it is qualified to upstream's protected
// resource: the same decision login and the consent card make, for surfaces
// that hold only a database handle. A client missing from the map does not
// own the resource.
func ResourceOwners(ctx context.Context, db remotesessions_repo.DBTX, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, upstream string) (map[uuid.UUID]bool, error) {
	owners, err := ResourceOwnersForServers(ctx, db, organizationID, []ResourceOwnerQuery{{ServerID: uuid.Nil, ProjectID: projectID, UserSessionIssuerID: userSessionIssuerID, Upstream: upstream}})
	if err != nil {
		return nil, err
	}
	return owners[uuid.Nil], nil
}

// ResourceOwnersForServers is ResourceOwners for many servers in two round
// trips, keyed by ServerID: one sibling listing for every distinct issuer
// and project, then one load of every sibling's attachments.
func ResourceOwnersForServers(ctx context.Context, db remotesessions_repo.DBTX, organizationID string, queries []ResourceOwnerQuery) (map[uuid.UUID]map[uuid.UUID]bool, error) {
	type pairKey struct{ issuer, project uuid.UUID }
	seen := map[pairKey]bool{}
	var issuerIDs, projectIDs []uuid.UUID
	for _, q := range queries {
		k := pairKey{issuer: q.UserSessionIssuerID, project: q.ProjectID}
		if !seen[k] {
			seen[k] = true
			issuerIDs = append(issuerIDs, q.UserSessionIssuerID)
			projectIDs = append(projectIDs, q.ProjectID)
		}
	}
	out := make(map[uuid.UUID]map[uuid.UUID]bool, len(queries))
	if len(issuerIDs) == 0 {
		return out, nil
	}
	rows, err := remotesessions_repo.New(db).ListRemoteSessionClientIDsForUserSessionIssuers(ctx, remotesessions_repo.ListRemoteSessionClientIDsForUserSessionIssuersParams{
		UserSessionIssuerIds: issuerIDs,
		ProjectIds:           projectIDs,
		OrganizationID:       organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list clients bound to user session issuers: %w", err)
	}
	siblings := map[pairKey][]uuid.UUID{}
	var allClients []uuid.UUID
	inAll := map[uuid.UUID]bool{}
	for _, r := range rows {
		k := pairKey{issuer: r.UserSessionIssuerID, project: r.ProjectID}
		siblings[k] = append(siblings[k], r.ClientID)
		if !inAll[r.ClientID] {
			inAll[r.ClientID] = true
			allClients = append(allClients, r.ClientID)
		}
	}
	attachments, err := attachmentsForClients(ctx, db, organizationID, allClients)
	if err != nil {
		return nil, err
	}
	for _, q := range queries {
		ids := siblings[pairKey{issuer: q.UserSessionIssuerID, project: q.ProjectID}]
		out[q.ServerID] = ownersAmong(attachments, ids, q.Upstream)
	}
	return out, nil
}

// ResourceOwnersAmong is ResourceOwners for a caller that already listed the
// clients bound to the login's user session issuer, as clientIDs.
func ResourceOwnersAmong(ctx context.Context, db remotesessions_repo.DBTX, organizationID string, clientIDs []uuid.UUID, upstream string) (map[uuid.UUID]bool, error) {
	if len(clientIDs) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	attachments, err := attachmentsForClients(ctx, db, organizationID, clientIDs)
	if err != nil {
		return nil, err
	}
	return ownersAmong(attachments, clientIDs, upstream), nil
}

func ownersAmong(attachments map[uuid.UUID][]remotesessions_repo.ListOrganizationMcpServersForClientRow, ids []uuid.UUID, upstream string) map[uuid.UUID]bool {
	// Siblings are judged among each other only, as at login.
	among := make(map[uuid.UUID][]remotesessions_repo.ListOrganizationMcpServersForClientRow, len(ids))
	for _, id := range ids {
		if rows, ok := attachments[id]; ok {
			among[id] = rows
		}
	}
	owners := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		resource := resourceAmongSiblings(among, id, upstream)
		owners[id] = resource != "" && sameUpstream(resource, upstream)
	}
	return owners
}
