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

	"github.com/speakeasy-api/gram/server/internal/attr"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
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
	if len(userIssuerIDs) > 0 {
		if _, err := mcpserversrepo.New(dbtx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
			UserSessionIssuerIds: userIssuerIDs,
			OrganizationID:       organizationID,
			ProjectID:            projectID,
		}); err != nil {
			return fmt.Errorf("resync mcp server remote session issuers: %w", err)
		}
	}
	if err := upstreamclients.Reconcile(ctx, dbtx, organizationID, projectID, userIssuerIDs); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers: %w", err)
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
	// Detached from request cancellation like RevokeAllDetached: already committed.
	ctx = context.WithoutCancel(ctx)

	bestEffortResyncProject(ctx, logger, db, organizationID, projectID, userIssuerIDs)
	if len(userIssuerIDs) == 0 {
		return
	}
	listCtx, cancel := context.WithTimeout(ctx, issuerResyncTimeout)
	sharing, err := mcpserversrepo.New(db).ListProjectsSharingOrganizationUserSessionIssuers(listCtx, mcpserversrepo.ListProjectsSharingOrganizationUserSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       organizationID,
	})
	cancel()
	if err != nil {
		logger.ErrorContext(ctx, "list projects sharing organization user session issuers", attr.SlogError(err))
		return
	}
	for _, sharingProjectID := range sharing {
		if sharingProjectID == projectID {
			continue
		}
		bestEffortResyncProject(ctx, logger, db, organizationID, sharingProjectID, userIssuerIDs)
	}
}

// bestEffortResyncProject gives each project its own deadline, so one slow
// project cannot starve the ones after it.
func bestEffortResyncProject(ctx context.Context, logger *slog.Logger, db mcpserversrepo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) {
	ctx, cancel := context.WithTimeout(ctx, issuerResyncTimeout)
	defer cancel()
	if err := ResyncMCPServerRemoteSessionIssuers(ctx, db, organizationID, projectID, userIssuerIDs); err != nil {
		logger.ErrorContext(ctx, "resync mcp server remote session issuers", attr.SlogError(err), attr.SlogProjectID(projectID.String()))
	}
}
