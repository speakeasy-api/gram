package mcpservers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// autoProvisionedIssuerSessionDuration is the issued-session lifetime for a
// user_session_issuer auto-created for a server that requires one. A plain
// default; operators tune it afterwards via the standard issuer-update API.
const autoProvisionedIssuerSessionDuration = 30 * 24 * time.Hour

// serverRequiresUserSessionIssuer reports whether the "private/tunneled ⇒
// issuer" invariant — enforced by the mcp_servers CHECK constraints — demands a
// user_session_issuer for a server with the given backend and visibility:
//
//   - Tunneled: always. A tunnel fronts a private network and is never served
//     anonymously.
//   - Remote: only when private. Public remote servers are anonymous.
//   - Toolset-backed: never here — their auth lives on
//     toolsets.user_session_issuer_id, not on the mcp_servers row.
func serverRequiresUserSessionIssuer(ids serverIDs, visibility string) bool {
	switch {
	case ids.TunneledMcpServerID.Valid:
		return true
	case ids.RemoteMcpServerID.Valid && visibility == VisibilityPrivate:
		return true
	default:
		return false
	}
}

// ensureServerUserSessionIssuer resolves the user_session_issuer id a server
// should be written with, auto-provisioning one when the invariant requires it
// and none was supplied. Precedence:
//
//  1. A caller-supplied issuer id is used as-is.
//  2. Otherwise, if the backend+visibility require an issuer
//     (serverRequiresUserSessionIssuer), a plain editable issuer is created in
//     the same transaction and its id returned.
//  3. Otherwise the (unset) input is returned — public/disabled remote and
//     toolset-backed servers carry no issuer here.
//
// The provisioned issuer is a normal user_session_issuer (default session
// duration, interactive challenge mode) slugged after the server, so operators
// manage it like any other. Runs inside the caller's transaction so a failed
// server write rolls the issuer back with it.
func ensureServerUserSessionIssuer(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	serverSlug string,
	ids serverIDs,
	visibility string,
) (uuid.NullUUID, error) {
	if ids.UserSessionIssuerID.Valid || !serverRequiresUserSessionIssuer(ids, visibility) {
		return ids.UserSessionIssuerID, nil
	}

	issuer, err := usersessionsrepo.New(dbtx).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               serverSlug,
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: autoProvisionedIssuerSessionDuration.Microseconds(),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("auto-provision user_session_issuer: %w", err)
	}
	return uuid.NullUUID{UUID: issuer.ID, Valid: true}, nil
}
