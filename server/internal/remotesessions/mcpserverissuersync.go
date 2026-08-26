// Keeps mcp_servers.remote_session_issuer_id tracking the client bindings it is
// derived from, so gateway token routing can use it as a lookup key.
//
// Best effort by design: the project-scoped client handlers recompute it after
// their transaction commits, so a crash or race leaves a stale value rather
// than a failed request. Stale degrades — the consent-time member lookup
// misses, the grant stays unqualified, and routing fails closed — and the next
// binding change or a backfill heals it. A WRONG value would misroute a bearer
// token, which is why the statement carries its own tenancy predicates.

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

const issuerResyncTimeout = 10 * time.Second

// ResyncMCPServerRemoteSessionIssuers recomputes the denormalised issuer for
// every MCP server in projectID carrying one of userIssuerIDs. Safe with
// issuers no server uses; only rows whose value differs are written. Ids
// outside the project derive nothing and write nothing.
func ResyncMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	// Ahead of the empty-set shortcut, so the guarantee does not depend on data.
	if organizationID == "" || projectID == uuid.Nil {
		return fmt.Errorf("resync mcp server remote session issuers: no tenant scope")
	}
	if len(userIssuerIDs) == 0 {
		return nil
	}

	if _, err := mcpserversrepo.New(dbtx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       organizationID,
		ProjectID:            projectID,
	}); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers: %w", err)
	}
	return nil
}

// BestEffortResyncMCPServerRemoteSessionIssuers is the post-commit form: the
// mutation it follows has already committed, so a failure is logged rather
// than returned and the stale value it leaves is recoverable.
func BestEffortResyncMCPServerRemoteSessionIssuers(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) {
	// Detached from request cancellation like RevokeAllDetached: already committed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issuerResyncTimeout)
	defer cancel()

	if err := ResyncMCPServerRemoteSessionIssuers(ctx, db, organizationID, projectID, userIssuerIDs); err != nil {
		logger.ErrorContext(ctx, "resync mcp server remote session issuers", attr.SlogError(err))
	}
}
