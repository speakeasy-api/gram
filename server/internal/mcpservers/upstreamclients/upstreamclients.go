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
// bindings after a binding, server or gateway membership change: it
// associates each client bound to one of userIssuerIDs with that issuer's
// server when the server is the issuer's only consumer, associates each
// gateway-fronted server with the one client its gateway issuer binds for
// the server's derived authorization server, then drops associations no
// issuer reaching the server still binds. Otherwise associations on an issuer
// shared by several servers are only added by explicit configuration or the
// backfill: the gateway capture is the one path that writes them, and only
// for the single client a gateway issuer binds for a server's derived
// authorization server.
//
// Each statement decides from the bindings as they are when it runs, and the
// prune runs last, so a reconcile that races a binding change and captures a
// binding removed meanwhile prunes it again before it returns.
func Reconcile(ctx context.Context, dbtx repo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil {
		return errors.New("reconcile mcp server upstream clients: no tenant scope")
	}
	q := repo.New(dbtx)
	if len(userIssuerIDs) > 0 {
		if _, err := q.CaptureSoleConsumerAssociations(ctx, repo.CaptureSoleConsumerAssociationsParams{
			OrganizationID:       organizationID,
			ProjectID:            projectID,
			UserSessionIssuerIds: userIssuerIDs,
		}); err != nil {
			return fmt.Errorf("capture mcp server upstream clients: %w", err)
		}
		if _, err := q.CaptureGatewayDerivedAssociations(ctx, repo.CaptureGatewayDerivedAssociationsParams{
			ProjectID:            projectID,
			OrganizationID:       organizationID,
			UserSessionIssuerIds: userIssuerIDs,
		}); err != nil {
			return fmt.Errorf("capture gateway upstream clients: %w", err)
		}
	}
	if _, err := q.PruneUnservableAssociations(ctx, repo.PruneUnservableAssociationsParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
	}); err != nil {
		return fmt.Errorf("prune mcp server upstream clients: %w", err)
	}
	return nil
}

// CredentialKindUserSession identifies a user session credential: the
// lineage of user_sessions rows that refresh rotation continues, named by
// COALESCE(credential_id, id).
const CredentialKindUserSession = "user_session"

// CredentialKindIssuanceProfile identifies an issuance selection profile:
// the choices a headless caller's new sessions are given, copied onto each
// once validated.
const CredentialKindIssuanceProfile = "issuance_profile"

// CredentialKindAPIKey identifies an agent API key credential by its
// api_keys row id. A rotation's replacement key gets the choices of the key
// it replaces.
const CredentialKindAPIKey = "api_key"

// CredentialKindExecutionProfileVersion identifies one version of an
// assistant's execution-selection profile, which the turn credentials it binds
// read their choices through.
const CredentialKindExecutionProfileVersion = "execution_profile_version"

// How an upstream client selection was made.
const (
	// SelectionSourceIssuance is a choice recorded when the credential was
	// issued.
	SelectionSourceIssuance = "issuance"

	// SelectionSourceBackfill is a choice recorded for an existing credential
	// by the offline backfill.
	SelectionSourceBackfill = "backfill"

	// SelectionSourceNewMember is a choice recorded for a gateway member
	// added after the credential was issued, the first time it was used.
	SelectionSourceNewMember = "new_member"

	// SelectionSourceAdmin is a choice an administrator made.
	SelectionSourceAdmin = "admin"
)
