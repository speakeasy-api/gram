package authclientbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// PreflightServerAssociations takes the gates SyncServerAssociations needs,
// for a caller that locks rows of its own before calling it. The gates have to
// come first: a caller holding one of those rows and then asking for a gate
// another transaction holds — while that one waits for the row — deadlocks the
// pair. SyncServerAssociations asks for the same gates again, which costs a
// transaction that already holds them nothing.
//
// The server set may still grow by the time the synchronization reads it, so
// this narrows the window rather than closing it; pass every server the caller
// knows it will decide.
func PreflightServerAssociations(ctx context.Context, tx pgx.Tx, organizationID string, serverIDs ...uuid.UUID) error {
	if organizationID == "" {
		return errors.New("preflight server associations: no tenant scope")
	}
	servers := dedupeServers(serverIDs)
	if len(servers) == 0 {
		return nil
	}
	_, err := lockOAuthSyncScope(ctx, New(tx), organizationID, servers)
	return err
}

// lockOAuthSyncScope serializes the OAuth registrations the given servers
// reach, and the typed clients those registrations already have, and reports
// the registrations it gated.
func lockOAuthSyncScope(ctx context.Context, q *Queries, organizationID string, servers []uuid.UUID) ([]uuid.UUID, error) {
	// The writer scope expands shared EMA registrations, not organization-wide
	// OAuth registrations. Serialize the complete OAuth registration set before
	// any typed client/link write, including clients that do not exist yet.
	registrations, err := q.ListOAuthClientsOfServers(ctx, ListOAuthClientsOfServersParams{OrganizationID: organizationID, McpServerIds: servers})
	if err != nil {
		return nil, fmt.Errorf("list OAuth synchronization scope: %w", err)
	}
	if err := q.LockOAuthRegistrationSyncScope(ctx, LockOAuthRegistrationSyncScopeParams{OrganizationID: organizationID, RegistrationIds: registrations}); err != nil {
		return nil, fmt.Errorf("lock OAuth registration synchronization scope: %w", err)
	}
	// Lock every existing candidate and stale client in UUID order before any
	// link insert can acquire a foreign-key lock. Administrator detaches use
	// this same client-before-link order. The registration gates cover new rows.
	if err := q.LockServerOAuthClientsForSync(ctx, LockServerOAuthClientsForSyncParams{OrganizationID: organizationID, McpServerIds: servers, RegistrationIds: registrations}); err != nil {
		return nil, fmt.Errorf("lock OAuth clients for synchronization: %w", err)
	}
	return registrations, nil
}

// dedupeServers drops the nil id and repeats, so a caller can pass whatever
// its discovery produced.
func dedupeServers(serverIDs []uuid.UUID) []uuid.UUID {
	servers := make([]uuid.UUID, 0, len(serverIDs))
	seen := make(map[uuid.UUID]bool, len(serverIDs))
	for _, id := range serverIDs {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		servers = append(servers, id)
	}
	return servers
}

// SyncServerAssociations records the typed auth clients and server links the
// given servers' upstream OAuth associations need, then brings the credential
// header copies on those servers' upstream OAuth and EMA clients in line, all
// inside the caller's transaction. Every write that adds an association runs
// it for the servers it associated, so a registration set up after an
// organization's backfill run is served by a typed client straight away
// rather than waiting for the next run.
//
// For each live association on the servers it makes the decision the backfill
// makes for that association, and only that one:
//
//   - The registration's credential owner picks the client type: a
//     subject-owned registration gets a remote_session client, a self-owned
//     one a service_account client. An unknown owner gets none.
//   - The client is the organization's backfilled client for the
//     registration, recorded under its stable id, scoped like the
//     registration, or revived under that id when it was retired.
//     Administrator-created clients are never reused, and a backfilled
//     client whose type or project disagrees with the registration is left
//     alone with no link.
//   - The server offers the client unless an administrator detached the
//     client from it; that detach is durable.
//
// It grants nothing. Personal clients are covered by the organization's
// personal auth client default, which this never restores once removed, and
// new service account clients require explicit authorization. Migration
// grants cover only clients reachable at migration time.
//
// A write whose evidence changed concurrently is decided once more. If the
// race persists, it returns a retryable error: the caller must roll back and
// retry the whole transaction, not commit an association with missing typed
// state. Deliberate security refusals are not races. The caller must acquire
// LockEMAWriterScope before its first row lock or mutation, including capture
// of legacy associations. This function writes OAuth clients/links before EMA
// copy synchronization acquires registration/client/link locks; acquiring the
// complete source/server scope here would be too late for earlier writes. For
// the same reason a caller that locks rows of its own first must take the
// registration gates through PreflightServerAssociations rather than leave
// them to the call that needs them.
func SyncServerAssociations(ctx context.Context, tx pgx.Tx, organizationID string, serverIDs ...uuid.UUID) error {
	ctx = withAtomicHeaderSync(ctx)
	if organizationID == "" {
		return errors.New("sync server associations: no tenant scope")
	}
	servers := dedupeServers(serverIDs)
	if len(servers) == 0 {
		return nil
	}
	q := New(tx)
	if _, err := lockOAuthSyncScope(ctx, q, organizationID, servers); err != nil {
		return err
	}
	opts := Options{OrganizationID: organizationID, Apply: true, PageSize: 0}
	// The report is the decision log the backfill keeps for its operator; a
	// sync writes the same records and discards the account of them.
	report := newReport(true)
	for attempt := range serverAssociationAttempts {
		raced, err := recordServerOAuthClients(ctx, tx, q, opts, &report, servers)
		if err != nil {
			return err
		}
		if !raced {
			break
		}
		if attempt == serverAssociationAttempts-1 {
			return oops.E(oops.CodeUnavailable, errRace, "upstream OAuth associations changed concurrently; retry the transaction")
		}
	}
	if err := q.RetireServerStaleBackfillLinks(ctx, RetireServerStaleBackfillLinksParams{OrganizationID: organizationID, McpServerIds: servers}); err != nil {
		return fmt.Errorf("retire stale OAuth links: %w", err)
	}
	// After all OAuth client/link writes, decide copies. Tombstoned typed
	// links still name registrations whose final legacy association was pruned.
	return syncDerivedCopies(ctx, tx, q, opts, &report, servers)
}

// serverAssociationAttempts bounds how often SyncServerAssociations decides
// again after a concurrent change.
const serverAssociationAttempts = 2

// recordServerOAuthClients records the auth client and link of every live
// association on the given servers, reporting whether any write's evidence
// changed before it.
func recordServerOAuthClients(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, servers []uuid.UUID) (bool, error) {
	decided := map[clientKey]uuid.UUID{}
	raced := false
	cursor := uuid.Nil
	for {
		rows, err := q.ListAuthClientBackfillCandidates(ctx, ListAuthClientBackfillCandidatesParams{
			Cursor:           cursor,
			OrganizationID:   opts.OrganizationID,
			OnlyMcpServerIds: servers,
			PageSize:         defaultPageSize,
		})
		if err != nil {
			return false, fmt.Errorf("list upstream OAuth associations of written servers: %w", err)
		}
		for _, row := range rows {
			result, err := decideAndWrite(ctx, tx, q, opts, row, decided, report)
			if err != nil {
				return false, err
			}
			if result.Outcome == OutcomeSkippedRace {
				raced = true
			}
			cursor = row.AssociationID
		}
		if len(rows) < defaultPageSize {
			return raced, nil
		}
	}
}
