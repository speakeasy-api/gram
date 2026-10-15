// identitydisable.go turns User Identity off for one MCP server: it stops that
// server signing anyone in, and takes the provider's client off the user
// session issuer only when no other server still signs in through it.
//
// A user session issuer shared by many servers is why this is its own
// operation. Unbinding every client the project can see would strip the
// sign-in of every other server on the issuer, and the server-level reconcile
// does not put a shared server's provider back, so each of them would have to
// be configured again by hand.

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// DisableServerIdentityResult is what turning User Identity off left in place.
type DisableServerIdentityResult struct {
	// Provider is the upstream authorization server the MCP server signed in
	// to, invalid when it already signed in to none.
	Provider uuid.NullUUID

	// ClientUnbound reports that the provider's client was taken off the user
	// session issuer, which signs out everyone holding a session through it.
	// False when another server still signs in through that client, which
	// keeps it bound.
	ClientUnbound bool
}

// ServerIdentityVersion is the server state a caller decided against, which
// every write to the row moves. Opaque: it is only ever compared, and only
// ever produced here, so a caller that shows a user one sign-in cannot turn
// off another.
func ServerIdentityVersion(server mcpserversrepo.McpServer) string {
	return server.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)
}

// DisableServerIdentity clears mcpServerID's upstream authorization server and
// unbinds that provider's client when this was the last server using it. It is
// idempotent: a server already signing in to nothing is left alone.
//
// expectedVersion is the ServerIdentityVersion the caller decided against, and
// is compared once this transaction holds the server row, so no configuration
// commit can land between the comparison and the clear. Empty skips the
// comparison, for a caller acting on the state it just read under the user's
// eyes; a caller whose decision was made elsewhere, as a confirmed agent tool
// call is, passes it and is refused with ErrIdentityStale when it moved.
//
// Locks in the order every identity write takes: the MCP server row, then the
// user session issuer, then the client rows a detach touches. Taking the
// issuer lock is also what makes the "last server using it" read safe against
// a concurrent identity commit, which holds the same lock.
//
// The cleared state is durable: nothing derives this column on a schedule, and
// the post-commit pass here only ever takes a provider away.
func (c *IdentityCommitter) DisableServerIdentity(ctx context.Context, scope IdentityScope, userIssuerID, mcpServerID uuid.UUID, expectedVersion string) (DisableServerIdentityResult, error) {
	var none DisableServerIdentityResult
	if scope.OrganizationID == "" || scope.ProjectID == uuid.Nil || userIssuerID == uuid.Nil || mcpServerID == uuid.Nil {
		return none, fmt.Errorf("disable server identity: incomplete scope")
	}

	tx, err := c.db.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin disable transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	servers := mcpserversrepo.New(tx)
	server, err := servers.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        mcpServerID,
		ProjectID: scope.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, identityRefusal(ErrIdentityNotFound, err, "MCP server not found")
	}
	if err != nil {
		return none, fmt.Errorf("lock MCP server: %w", err)
	}
	if !server.UserSessionIssuerID.Valid || server.UserSessionIssuerID.UUID != userIssuerID {
		return none, identityRefusal(ErrIdentityInvalid, nil, "MCP server does not use this user session issuer")
	}
	// Under the row lock, so a configuration commit either committed before
	// this read — and is seen here — or is still waiting for the row this
	// transaction now holds and clears.
	if expectedVersion != "" && ServerIdentityVersion(server) != expectedVersion {
		return none, identityRefusal(ErrIdentityStale, nil, "MCP server sign-in changed since it was read")
	}
	if !server.RemoteSessionIssuerID.Valid {
		// Nothing to clear and no client of this server's to unbind. Commit so
		// the lock is released rather than rolled back under the caller.
		if err := tx.Commit(ctx); err != nil {
			return none, fmt.Errorf("commit disable transaction: %w", err)
		}
		return none, nil
	}
	provider := server.RemoteSessionIssuerID

	q := repo.New(tx)
	if err := lockUserSessionIssuers(ctx, tx, q, scope.ProjectID, scope.OrganizationID, []uuid.UUID{userIssuerID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return none, identityRefusal(ErrIdentityNotFound, err, "user session issuer not found")
		}
		return none, err
	}
	issuer, err := q.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             userIssuerID,
		ProjectID:      scope.ProjectID,
		OrganizationID: scope.OrganizationID,
	})
	if err != nil {
		return none, fmt.Errorf("get locked user session issuer: %w", err)
	}
	if _, err := q.LockProjectUserIssuerForDetach(ctx, repo.LockProjectUserIssuerForDetachParams{
		ID:             userIssuerID,
		ProjectID:      scope.ProjectID,
		OrganizationID: scope.OrganizationID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return none, identityRefusal(ErrIdentityNotFound, err, "user session issuer not found")
	} else if err != nil {
		return none, fmt.Errorf("lock user session issuer row: %w", err)
	}

	unbound, err := c.unbindDisabledProvider(ctx, tx, q, scope, issuer, userIssuerID, mcpServerID, provider.UUID)
	if err != nil {
		return none, err
	}

	cleared, err := servers.StampMCPServerRemoteSessionIssuer(ctx, mcpserversrepo.StampMCPServerRemoteSessionIssuerParams{
		RemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ID:                    mcpServerID,
		ProjectID:             scope.ProjectID,
		UserSessionIssuerID:   uuid.NullUUID{UUID: userIssuerID, Valid: true},
	})
	if err != nil {
		return none, fmt.Errorf("clear MCP server remote session issuer: %w", err)
	}
	if cleared == 0 {
		return none, identityRefusal(ErrIdentityConflict, nil, "MCP server identity configuration changed while preparing the request")
	}

	disabled, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        mcpServerID,
		ProjectID: scope.ProjectID,
	})
	if err != nil {
		return none, fmt.Errorf("reload disabled MCP server: %w", err)
	}
	if err := c.audit.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          scope.OrganizationID,
		ProjectID:               scope.ProjectID,
		Actor:                   scope.Actor,
		ActorDisplayName:        scope.ActorDisplayName,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(mcpServerID),
		McpServerName:           conv.FromPGTextOrEmpty[string](disabled.Name),
		McpServerSlug:           conv.FromPGTextOrEmpty[string](disabled.Slug),
		McpServerSnapshotBefore: mv.BuildMcpServerView(server),
		McpServerSnapshotAfter:  mv.BuildMcpServerView(disabled),
	}); err != nil {
		return none, fmt.Errorf("audit MCP server identity disable: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return none, fmt.Errorf("commit disable transaction: %w", err)
	}
	BestEffortClearUnboundMCPServerRemoteSessionIssuers(ctx, c.logger, c.db, scope.OrganizationID, scope.ProjectID, []uuid.UUID{userIssuerID})
	return DisableServerIdentityResult{Provider: provider, ClientUnbound: unbound}, nil
}

// unbindDisabledProvider takes the provider's clients off the user session
// issuer, and only when no other live server in the organization signs in
// through that provider: those servers route by provider, so unbinding its one
// client would strand them.
func (c *IdentityCommitter) unbindDisabledProvider(ctx context.Context, tx pgx.Tx, q *repo.Queries, scope IdentityScope, issuer repo.GetUserSessionIssuerForProjectRow, userIssuerID, mcpServerID, providerID uuid.UUID) (bool, error) {
	others, err := q.CountOrganizationMCPServersWithRemoteSessionIssuer(ctx, repo.CountOrganizationMCPServersWithRemoteSessionIssuerParams{
		UserSessionIssuerID:   uuid.NullUUID{UUID: userIssuerID, Valid: true},
		RemoteSessionIssuerID: uuid.NullUUID{UUID: providerID, Valid: true},
		ExcludedMcpServerID:   mcpServerID,
		OrganizationID:        scope.OrganizationID,
	})
	if err != nil {
		return false, fmt.Errorf("count MCP servers sharing the provider: %w", err)
	}
	if others > 0 {
		return false, nil
	}

	bound, err := listUserSessionIssuerClients(ctx, q, scope.ProjectID, scope.OrganizationID, userIssuerID)
	if err != nil {
		return false, err
	}
	detach := identityDetachScope{scope: scope, userIssuerID: userIssuerID, userIssuerOrgLevel: !issuer.ProjectID.Valid}
	unbound := false
	for _, client := range bound {
		if client.RemoteSessionIssuerID != providerID {
			continue
		}
		if err := detachClientFromUserSessionIssuer(ctx, tx, q, c.audit, detach, client); err != nil {
			return false, err
		}
		unbound = true
	}
	return unbound, nil
}
