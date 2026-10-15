// Keeps mcp_servers.remote_session_issuer_id, the upstream authorization
// server an MCP server's users sign in to, consistent with the client bindings
// it is reached through, so gateway token routing can use it as a lookup key.
//
// The column is a per-server fact: an identity commit writes it on the one
// server it configures (StampMCPServerRemoteSessionIssuer), and it stands as
// long as a client for it is bound. Deriving it per user session issuer
// instead would let one server's OAuth upstream repoint or blank every other
// server sharing that issuer, which an organization-level issuer has many of.
//
// A server alone on its user session issuer, and holding no provider a client
// is still bound for, is derived from the single provider bound there: there
// is no sibling to borrow from, and it is the only configuration a
// toolset-backed or tunneled server has.
//
// Best effort by design: the project-scoped client handlers run it after their
// transaction commits, so a crash or race leaves a stale value rather than a
// failed request. Stale degrades — the consent-time member lookup misses, the
// grant stays unqualified, and routing fails closed — and the next binding
// change or a backfill heals it. A WRONG value would misroute a bearer token,
// which is why the statement carries its own tenancy predicates.

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

const issuerReconcileTimeout = 10 * time.Second

// ReconcileMCPServerRemoteSessionIssuers brings every MCP server in projectID
// carrying one of userIssuerIDs back in line with the clients bound there: it
// keeps the provider configured on a server while a client for it is bound,
// and otherwise derives one only for a server alone on its issuer. Safe with
// issuers no server uses; ids outside the project reach nothing.
func ReconcileMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	// Ahead of the empty-set shortcut, so the guarantee does not depend on data.
	if organizationID == "" || projectID == uuid.Nil {
		return fmt.Errorf("reconcile mcp server remote session issuers: no tenant scope")
	}
	if len(userIssuerIDs) == 0 {
		return nil
	}

	if _, err := mcpserversrepo.New(dbtx).ReconcileMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ReconcileMCPServerRemoteSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       organizationID,
		ProjectID:            projectID,
	}); err != nil {
		return fmt.Errorf("reconcile mcp server remote session issuers: %w", err)
	}
	return nil
}

// BestEffortReconcileMCPServerRemoteSessionIssuers is the post-commit form: the
// mutation it follows has already committed, so a failure is logged rather
// than returned and the stale value it leaves is recoverable.
func BestEffortReconcileMCPServerRemoteSessionIssuers(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) {
	// Detached from request cancellation like RevokeAllDetached: already committed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issuerReconcileTimeout)
	defer cancel()

	if err := ReconcileMCPServerRemoteSessionIssuers(ctx, db, organizationID, projectID, userIssuerIDs); err != nil {
		logger.ErrorContext(ctx, "reconcile mcp server remote session issuers", attr.SlogError(err))
	}
}
