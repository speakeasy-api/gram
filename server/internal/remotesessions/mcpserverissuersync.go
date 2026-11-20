// Keeps mcp_servers.remote_session_issuer_id tracking the client associations
// and bindings it is derived from, so gateway token routing can use it as a
// lookup key.
//
// Best effort by design: the project-scoped client handlers recompute it after
// their transaction commits, so a crash or race leaves a stale value rather
// than a failed request. Stale degrades — the consent-time member lookup
// misses, the grant stays unqualified, and routing fails closed — and the next
// binding change or a backfill heals it. A WRONG value would misroute a bearer
// token, which is why the statement carries its own tenancy predicates.
//
// The same pass reconciles the MCP server upstream client associations derived
// from these bindings, and derives the column on both sides of that.

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
// every MCP server in projectID carrying one of userIssuerIDs, reconciles the
// project's upstream client associations, then recomputes the issuer from the
// result. Safe with issuers no server uses, and with none at all, which only
// prunes associations; only rows whose value differs are written. Ids outside
// the project derive nothing and write nothing.
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
	derive := func() error {
		if len(userIssuerIDs) == 0 {
			return nil
		}
		if _, err := mcpserversrepo.New(tx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
			UserSessionIssuerIds: userIssuerIDs,
			OrganizationID:       organizationID,
			ProjectID:            projectID,
		}); err != nil {
			return fmt.Errorf("resync mcp server remote session issuers: %w", err)
		}
		return nil
	}
	// Gateway capture reads the column, so derive it before reconcile, and
	// again after, because reconcile changes the associations it derives from.
	if err := derive(); err != nil {
		return err
	}
	if err := upstreamclients.Reconcile(ctx, tx, organizationID, projectID, userIssuerIDs); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers: %w", err)
	}
	if err := derive(); err != nil {
		return err
	}
	if !existing {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer resynchronization: %w", err)
		}
	}
	return nil
}

// ResyncMCPServerRemoteSessionIssuersForConsolidation is the form issuer
// consolidation runs in its own transaction, after it has moved the source's
// servers and bindings and retired the source. It reconciles the project's
// associations without the gateway capture, then derives the issuer once from
// the result: the capture is the only reconcile step that reads the derived
// value, and consolidation records what configuration proves before the move
// instead of letting it decide from the merged bindings.
//
// The caller must hold LockEMAWriterScope for projectID before its first
// mutation or row lock.
func ResyncMCPServerRemoteSessionIssuersForConsolidation(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil {
		return fmt.Errorf("resync mcp server remote session issuers for consolidation: no tenant scope")
	}
	if err := upstreamclients.ReconcileWithoutGatewayCaptureInLockedTransaction(ctx, tx, organizationID, projectID, userIssuerIDs); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers for consolidation: %w", err)
	}
	if len(userIssuerIDs) == 0 {
		return nil
	}
	if _, err := mcpserversrepo.New(tx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       organizationID,
		ProjectID:            projectID,
	}); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers for consolidation: %w", err)
	}
	return nil
}

// BestEffortResyncMCPServerRemoteSessionIssuers is the post-commit form: the
// mutation it follows has already committed, so a failure is logged rather
// than returned and the stale value it leaves is recoverable.
//
// It resyncs projectID, then every other project with a server on one of the
// organization-level issuers among userIssuerIDs: an organization admin can
// change those bindings from one project, and every project's servers share
// them.
func BestEffortResyncMCPServerRemoteSessionIssuers(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) {
	bestEffortUpdateMCPServerRemoteSessionIssuers(context.WithoutCancel(ctx), logger, db, organizationID, projectID, userIssuerIDs, ResyncMCPServerRemoteSessionIssuers)
}

// BestEffortRestampMCPServerRemoteSessionIssuers refreshes routing in each
// sharing project without capture or pruning of OAuth offers.
func BestEffortRestampMCPServerRemoteSessionIssuers(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) {
	if len(userIssuerIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issuerResyncTimeout)
	defer cancel()
	bestEffortUpdateMCPServerRemoteSessionIssuers(ctx, logger, db, organizationID, projectID, userIssuerIDs, RestampMCPServerRemoteSessionIssuers)
}

func bestEffortUpdateMCPServerRemoteSessionIssuers(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID, update func(context.Context, mcpserversrepo.DBTX, string, uuid.UUID, []uuid.UUID) error) {
	// Each update is bounded by both its own and any aggregate deadline.
	updateProject := func(id uuid.UUID) {
		projectCtx, cancel := context.WithTimeout(ctx, issuerResyncTimeout)
		defer cancel()
		if err := update(projectCtx, db, organizationID, id, userIssuerIDs); err != nil {
			logger.ErrorContext(ctx, "refresh mcp server remote session issuers", attr.SlogError(err), attr.SlogProjectID(id.String()))
		}
	}
	updateProject(projectID)
	if len(userIssuerIDs) == 0 {
		return
	}
	listCtx, cancel := context.WithTimeout(ctx, issuerResyncTimeout)
	sharing, err := mcpserversrepo.New(db).ListProjectsSharingOrganizationUserSessionIssuers(listCtx, mcpserversrepo.ListProjectsSharingOrganizationUserSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs, OrganizationID: organizationID,
	})
	cancel()
	if err != nil {
		logger.ErrorContext(ctx, "list projects sharing organization user session issuers", attr.SlogError(err))
		return
	}
	for _, id := range sharing {
		if ctx.Err() != nil {
			break
		}
		if id != projectID {
			updateProject(id)
		}
	}
}

// RestampMCPServerRemoteSessionIssuers updates the routing copy without using
// issuer links to capture or prune server offers.
func RestampMCPServerRemoteSessionIssuers(ctx context.Context, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if len(userIssuerIDs) == 0 {
		return nil
	}
	if _, err := mcpserversrepo.New(db).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
		OrganizationID: organizationID, ProjectID: projectID, UserSessionIssuerIds: userIssuerIDs,
	}); err != nil {
		return fmt.Errorf("restamp server remote session issuers: %w", err)
	}
	return nil
}
