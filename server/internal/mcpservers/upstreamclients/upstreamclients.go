// Package upstreamclients records which upstream OAuth clients (remote
// session clients) each MCP server uses.
//
// An association is a configuration fact about one server, unlike a client's
// binding to a user session issuer, which every server sharing that issuer
// inherits. Associations are written ahead of the serving path that will read
// them: until then they are kept to the combinations the issuer bindings would
// serve, so an association never names a client the current runtime would
// refuse for that server.
package upstreamclients

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
)

// Reconcile brings the project's associations in line with its issuer
// bindings after a binding or server change: it drops associations whose
// client is no longer bound to the server's user session issuer, then
// associates each client bound to one of userIssuerIDs with that issuer's
// server when the server is the issuer's only consumer. Associations on an
// issuer shared by several servers are only ever added by explicit
// configuration or the offline backfill.
func Reconcile(ctx context.Context, dbtx repo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil {
		return errors.New("reconcile mcp server upstream clients: no tenant scope")
	}
	q := repo.New(dbtx)
	if _, err := q.PruneUnservableAssociations(ctx, repo.PruneUnservableAssociationsParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
	}); err != nil {
		return fmt.Errorf("prune mcp server upstream clients: %w", err)
	}
	if len(userIssuerIDs) == 0 {
		return nil
	}
	if _, err := q.CaptureSoleConsumerAssociations(ctx, repo.CaptureSoleConsumerAssociationsParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		UserSessionIssuerIds: userIssuerIDs,
	}); err != nil {
		return fmt.Errorf("capture mcp server upstream clients: %w", err)
	}
	return nil
}

// CredentialKindUserSession identifies a user session credential: the
// lineage of user_sessions rows that refresh rotation continues, named by
// COALESCE(credential_id, id).
const CredentialKindUserSession = "user_session"

// How an upstream client selection was made.
const (
	// SelectionSourceIssuance is a choice recorded when the credential was
	// issued.
	SelectionSourceIssuance = "issuance"

	// SelectionSourceBackfill is a choice recorded for an existing credential
	// by the offline backfill.
	SelectionSourceBackfill = "backfill"
)
