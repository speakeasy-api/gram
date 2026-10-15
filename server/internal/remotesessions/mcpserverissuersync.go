// Keeps mcp_servers.remote_session_issuer_id, the upstream authorization
// server an MCP server's users sign in to, consistent with the client
// bindings it is reached through, so gateway token routing can use it as a
// lookup key.
//
// Nothing here derives the column, ever. It is written explicitly, by the
// identity commit for the server it configures and by the per-server disable
// that clears it — for every backend, so a toolset-backed or tunneled server
// is configured the same way a Remote MCP-backed one is. A server signing in
// to nothing stays that way until an operator configures it, which is what
// makes a disable durable: no pass can hand it a provider nobody chose.
//
// ClearUnbound is the one automatic write, and it only takes a provider away:
// a server whose own client is gone must fail closed rather than route a
// bearer to an upstream its users cannot sign in to. It reads the candidates
// and then clears each one against the provider it observed, so a
// configuration committed in between keeps its value instead of being
// overwritten by a decision made before it existed.
//
// Best effort by design: the project-scoped client handlers run these after
// their transaction commits, so a crash or race leaves a stale value rather
// than a failed request. Stale degrades — the consent-time member lookup
// misses, the grant stays unqualified, and routing fails closed.

package remotesessions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

const issuerWriteTimeout = 10 * time.Second

// ClearUnboundMCPServerRemoteSessionIssuers clears the provider of every MCP
// server in projectID carrying one of userIssuerIDs whose provider has no live
// client bound there. Each clear is conditional on the row still being the one
// read — same provider, same version — so it never overwrites a configuration
// committed after the candidates were read, including one that reconfigures
// the provider it already named. Safe with issuers no server uses; ids outside
// the project reach nothing.
func ClearUnboundMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	// Ahead of the empty-set shortcut, so the guarantee does not depend on data.
	if organizationID == "" || projectID == uuid.Nil {
		return fmt.Errorf("clear unbound mcp server remote session issuers: no tenant scope")
	}
	if len(userIssuerIDs) == 0 {
		return nil
	}

	q := mcpserversrepo.New(dbtx)
	stale, err := q.ListMCPServerRemoteSessionIssuersToClear(ctx, mcpserversrepo.ListMCPServerRemoteSessionIssuersToClearParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       organizationID,
		ProjectID:            projectID,
	})
	if err != nil {
		return fmt.Errorf("list mcp server remote session issuers to clear: %w", err)
	}
	for _, server := range stale {
		if _, err := q.ClearMCPServerRemoteSessionIssuerIfUnchanged(ctx, mcpserversrepo.ClearMCPServerRemoteSessionIssuerIfUnchangedParams{
			ID:                            server.ID,
			ProjectID:                     projectID,
			OrganizationID:                organizationID,
			ObservedRemoteSessionIssuerID: server.RemoteSessionIssuerID,
			ObservedUpdatedAt:             server.ObservedUpdatedAt,
		}); err != nil {
			return fmt.Errorf("clear mcp server remote session issuer: %w", err)
		}
	}
	return nil
}

// BestEffortClearUnboundMCPServerRemoteSessionIssuers is the post-commit clear
// on its own, for a write that takes a provider away and must not hand the
// server a different one.
func BestEffortClearUnboundMCPServerRemoteSessionIssuers(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issuerWriteTimeout)
	defer cancel()

	if err := ClearUnboundMCPServerRemoteSessionIssuers(ctx, db, organizationID, projectID, userIssuerIDs); err != nil {
		logger.ErrorContext(ctx, "clear unbound mcp server remote session issuers", attr.SlogError(err))
	}
}
