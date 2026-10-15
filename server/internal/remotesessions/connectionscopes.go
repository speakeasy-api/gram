package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// ClientConnectionScopes is what a sign-in through one client bound to a user
// session issuer requests: the client's own inputs over the cached resource
// row when the client's grant is qualified to that resource (owns), else over
// no resource row.
func ClientConnectionScopes(r repo.ListRemoteSessionClientsForUserSessionIssuerRow, cached ResourceScopes, owns bool) ScopeResolution {
	resource := ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: cached.UseDiscovered}
	if owns {
		resource = cached
	}
	return (Client{ //nolint:exhaustruct // Only scope inputs are used by RequestedScopes.
		ID:                      r.ClientID,
		ClientScope:             r.ClientScope,
		IssuerScopesSupported:   r.ScopesSupported,
		IssuerScopeOverride:     r.ScopeOverride,
		IssuerOmitScopeFallback: r.OmitScopeFallback.Valid && r.OmitScopeFallback.Bool,
	}).RequestedScopes(resource)
}

// ConnectionScopes is ClientConnectionScopes for clientID among the clients
// bound to userSessionIssuerID, from cached rows only; it never probes.
// Identity chaining requests these scopes, so a chained token and an
// interactive sign-in on the same server ask for the same access. found is
// false when clientID is not bound to the issuer.
func ConnectionScopes(ctx context.Context, db repo.DBTX, projectID uuid.UUID, organizationID string, userSessionIssuerID, clientID uuid.UUID, resourceURL string) (resolution ScopeResolution, found bool, err error) {
	var none ScopeResolution
	rows, err := repo.New(db).ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
		OrganizationID:      conv.ToPGText(organizationID),
	})
	if err != nil {
		return none, false, fmt.Errorf("list remote session clients: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	var row *repo.ListRemoteSessionClientsForUserSessionIssuerRow
	for i := range rows {
		ids = append(ids, rows[i].ClientID)
		if rows[i].ClientID == clientID {
			row = &rows[i]
		}
	}
	if row == nil {
		return none, false, nil
	}
	cached, _, err := CachedResourceScopes(ctx, db, projectID, resourceURL, true)
	if err != nil {
		return none, false, err
	}
	owners, err := ResourceOwnersAmong(ctx, db, organizationID, ids, resourceURL)
	if err != nil {
		return none, false, fmt.Errorf("decide resource ownership: %w", err)
	}
	return ClientConnectionScopes(*row, cached, owners[clientID]), true, nil
}
