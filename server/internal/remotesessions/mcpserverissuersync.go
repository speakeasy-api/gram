// Keeps mcp_servers.remote_session_issuer_id tracking the client bindings it is
// derived from, so gateway token routing can use it as a lookup key.
//
// Best effort by design: the project-scoped client handlers recompute it after
// their transaction commits, so a crash or race leaves a stale value rather
// than a failed request. Stale degrades — the consent-time member lookup
// misses, the grant stays unqualified, and routing fails closed — and the next
// binding change or a backfill heals it. A WRONG value would misroute a bearer
// token, which is why the statement carries its own tenancy predicates.
//
// The same pass reconciles the MCP server upstream client associations
// derived from these bindings, so every binding write keeps both in step.

package remotesessions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

const issuerResyncTimeout = 10 * time.Second

// ResyncMCPServerRemoteSessionIssuers recomputes the denormalised issuer for
// every MCP server in projectID carrying one of userIssuerIDs, then reconciles
// the project's upstream client associations. Safe with issuers no server
// uses, and with none at all, which only prunes associations; only rows whose
// value differs are written. Ids outside the project derive nothing and write
// nothing.
func ResyncMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil {
		return fmt.Errorf("resync mcp server remote session issuers: no tenant scope")
	}
	tx, existing := dbtx.(pgx.Tx)
	if !existing {
		beginner, ok := dbtx.(interface {
			Begin(context.Context) (pgx.Tx, error)
		})
		if !ok {
			return fmt.Errorf("resync mcp server remote session issuers: %T cannot begin a transaction", dbtx)
		}
		var err error
		tx, err = beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin issuer resynchronization: %w", err)
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	}
	// The denormalized server update also takes row locks. Acquire the same
	// complete scope before it and retain it through capture and typed copies.
	// A caller supplying a transaction must preflight before its earlier writes.
	if err := authclientbackfill.LockEMAWriterScope(ctx, tx, organizationID, []uuid.UUID{projectID}); err != nil {
		return fmt.Errorf("lock issuer resynchronization scope: %w", err)
	}
	if len(userIssuerIDs) > 0 {
		if _, err := mcpserversrepo.New(tx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
			UserSessionIssuerIds: userIssuerIDs,
			OrganizationID:       organizationID,
			ProjectID:            projectID,
		}); err != nil {
			return fmt.Errorf("resync mcp server remote session issuers: %w", err)
		}
	}
	if err := upstreamclients.Reconcile(ctx, tx, organizationID, projectID, userIssuerIDs); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers: %w", err)
	}
	if !existing {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer resynchronization: %w", err)
		}
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
