// Keeps mcp_servers.remote_session_issuer_id tracking the client bindings it
// is derived from. The column denormalises "which authorization server does
// this server's upstream authenticate against", which is otherwise only
// reachable by walking user_session_issuer -> bindings -> clients -> issuer.
// Gateway token routing needs it as a lookup key, and a join at read time
// cannot answer it per member without that walk.
//
// Every path that creates, moves, or removes a client binding has to call
// this, in its own transaction, or the column silently rots: clients and
// issuers are only ever soft-deleted, so the FK's ON DELETE SET NULL never
// fires and nothing else clears a stale value.

package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// ResyncMCPServerRemoteSessionIssuers recomputes the denormalised issuer for
// every MCP server carrying one of userIssuerIDs. Safe to call with issuers
// that no server uses, and safe to call when nothing changed — the statement
// only writes rows whose value actually differs.
//
// Callers pass the issuers whose bindings they touched. For a detach or a
// delete that means capturing them before the rows go, since afterwards there
// is nothing left to walk.
func ResyncMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, userIssuerIDs []uuid.UUID) error {
	if len(userIssuerIDs) == 0 {
		return nil
	}
	if _, err := mcpserversrepo.New(dbtx).ResyncMCPServerRemoteSessionIssuers(ctx, userIssuerIDs); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers: %w", err)
	}
	return nil
}
