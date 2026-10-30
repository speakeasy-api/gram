package authclientbackfill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The credential source kinds an MCP server can be backed by.
const (
	SourceKindRemote   = "remote"
	SourceKindTunneled = "tunneled"
)

// ErrSourceNotFound reports a source outside the organization.
var ErrSourceNotFound = errors.New("credential source not found in organization")

// LockRemoteSource takes the row lock SyncRemoteSource needs, so a caller that
// locks one of the source's MCP servers can take it first and keep every
// path's lock order source-then-server. SyncRemoteSource takes the same lock,
// so a caller that locks no server need not call this. ErrSourceNotFound means
// the source is not the organization's.
func LockRemoteSource(ctx context.Context, tx pgx.Tx, organizationID string, sourceID uuid.UUID) error {
	return lockSource(ctx, tx, organizationID, SourceKindRemote, sourceID)
}

// LockTunneledSource is LockRemoteSource for a tunneled source.
func LockTunneledSource(ctx context.Context, tx pgx.Tx, organizationID string, sourceID uuid.UUID) error {
	return lockSource(ctx, tx, organizationID, SourceKindTunneled, sourceID)
}

func lockSource(ctx context.Context, tx pgx.Tx, organizationID, kind string, sourceID uuid.UUID) error {
	if organizationID == "" || sourceID == uuid.Nil {
		return errors.New("lock credential source: no tenant scope")
	}
	q := New(tx)
	var err error
	switch kind {
	case SourceKindRemote:
		_, err = q.LockRemoteCredentialSource(ctx, LockRemoteCredentialSourceParams{OrganizationID: organizationID, SourceID: sourceID})
	case SourceKindTunneled:
		_, err = q.LockTunneledCredentialSource(ctx, LockTunneledCredentialSourceParams{OrganizationID: organizationID, SourceID: sourceID})
	default:
		return fmt.Errorf("lock credential source: unknown source kind %q", kind)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSourceNotFound
	}
	if err != nil {
		return fmt.Errorf("lock %s source %s: %w", kind, sourceID, err)
	}
	return nil
}

// SyncRemoteSource brings the auth clients derived from one remote source's
// credential headers in line with its headers and live servers, inside the
// caller's transaction, as the header and server writes that change them
// happen. It makes the same decisions as the backfill for that source, so a
// header set up today gets its typed records without waiting for the next
// backfill run:
//
//   - A source with one Authorization header and live servers gets its
//     static_credential or pass_through client under the stable id, carrying
//     all its credential headers as copies and offered by each live server on
//     the source, unless an administrator detached it from that server.
//   - A source without an Authorization header gets a standalone client only
//     for servers whose direct route has no upstream OAuth or EMA owner.
//   - Every other client derived from the source's headers is retired with its
//     copies and backfilled links, as are backfilled links from servers no
//     longer on the source.
//   - The source's other credential headers, copied onto the auth clients of
//     the upstream OAuth and EMA clients its servers offer, are brought in
//     line too, under the backfill's own ownership rule: a client whose
//     servers sit on more than one source with credential headers carries
//     none of them.
//
// Newly linked servers whose need for upstream authentication is undecided
// are marked as needing it. Grants, credential choices, administrator clients
// and administrator links are left alone. A decision whose evidence changed
// during the sync is an error, so the caller's write is rolled back rather
// than left with records that do not match its configuration.
//
// This takes the source row lock, which every writer of a source takes before
// touching its headers or its servers. A caller that locks one of the source's
// MCP servers first must call LockRemoteSource before that lock, so the two
// never wait on each other in opposite order.
func SyncRemoteSource(ctx context.Context, tx pgx.Tx, organizationID string, sourceID uuid.UUID) error {
	return syncSource(ctx, tx, organizationID, SourceKindRemote, sourceID)
}

// SyncTunneledSource is SyncRemoteSource for a tunneled source. No management
// API writes a tunneled source's headers, so in practice it keeps the records
// of a tunneled source's lifecycle: its deletion, and servers arriving on or
// leaving it, must not leave a derived client, link or header copy behind.
func SyncTunneledSource(ctx context.Context, tx pgx.Tx, organizationID string, sourceID uuid.UUID) error {
	return syncSource(ctx, tx, organizationID, SourceKindTunneled, sourceID)
}

// SyncServerDerivedCopies brings the credential header copies on the upstream
// OAuth and EMA clients the given servers offer in line with the sources those
// servers sit on now. A server that has just moved between sources, lost its
// backend or been deleted still names its clients, so the copies of the source
// it left are retired.
//
// EMA copies lock their upstream registration and reload its live servers
// before deciding source ownership. Conditional copy failures abort the caller
// transaction rather than leaving partially synchronized credentials. Writers
// changing reach acquire the complete project/source/server scope before their
// first mutation, then registrations, typed clients, and links in that order.
func SyncServerDerivedCopies(ctx context.Context, tx pgx.Tx, organizationID string, serverIDs ...uuid.UUID) error {
	ctx = withAtomicHeaderSync(ctx)
	if organizationID == "" {
		return errors.New("sync server derived copies: no tenant scope")
	}
	opts := Options{OrganizationID: organizationID, Apply: true, PageSize: 0}
	report := newReport(true)
	return syncDerivedCopies(ctx, tx, New(tx), opts, &report, serverIDs)
}

func syncSource(ctx context.Context, tx pgx.Tx, organizationID, kind string, sourceID uuid.UUID) error {
	ctx = withAtomicHeaderSync(ctx)
	if err := lockSource(ctx, tx, organizationID, kind, sourceID); err != nil {
		return err
	}
	q := New(tx)

	opts := Options{OrganizationID: organizationID, Apply: true, PageSize: 0}
	// The report is the decision log the backfill keeps for its operator; a
	// sync writes the same records and discards the account of them.
	report := newReport(true)
	want := newExpected()

	rows, err := q.ListCredentialSources(ctx, ListCredentialSourcesParams{
		Cursor:         uuid.Nil,
		OnlySourceID:   uuid.NullUUID{UUID: sourceID, Valid: true},
		PageSize:       1,
		OrganizationID: organizationID,
	})
	if err != nil {
		return fmt.Errorf("read credential headers of %s source %s: %w", kind, sourceID, err)
	}
	// No row means the source, its project or its last credential header is
	// gone: nothing is wanted and everything derived from it is retired below.
	if len(rows) == 1 {
		row := rows[0]
		if row.SourceKind != kind {
			return fmt.Errorf("sync auth clients of %s source %s: read a %s source", kind, sourceID, row.SourceKind)
		}
		if err := syncSourceClient(ctx, tx, q, opts, row, &report, want); err != nil {
			return err
		}
	}
	if len(want.raced) > 0 {
		return fmt.Errorf("sync auth clients of %s source %s: configuration changed concurrently", kind, sourceID)
	}

	if err := retireSourceClients(ctx, q, organizationID, kind, sourceID, want); err != nil {
		return err
	}

	// Before the copies below, so this writer takes mcp_servers rows ahead of
	// mcp_auth_client_headers rows, the order an association write takes them
	// in. Neither step reads what the other writes.
	if err := markLinkedServers(ctx, q, organizationID, want); err != nil {
		return err
	}

	// The servers on the source name the upstream OAuth and EMA clients whose
	// copies of its other credential headers this write changed. Tombstoned
	// ones are among them, so a source losing its last server still retires
	// the copies that server's clients carried.
	servers, err := sourceServerIDs(ctx, q, organizationID, kind, sourceID)
	if err != nil {
		return err
	}

	return syncDerivedCopies(ctx, tx, q, opts, &report, servers)
}

// sourceServerIDs is every MCP server backed by one source, tombstoned ones
// included.
func sourceServerIDs(ctx context.Context, q *Queries, organizationID, kind string, sourceID uuid.UUID) ([]uuid.UUID, error) {
	switch kind {
	case SourceKindRemote:
		ids, err := q.ListRemoteSourceServerIDs(ctx, ListRemoteSourceServerIDsParams{OrganizationID: organizationID, SourceID: sourceID})
		if err != nil {
			return nil, fmt.Errorf("list servers of remote source %s: %w", sourceID, err)
		}
		return ids, nil
	case SourceKindTunneled:
		ids, err := q.ListTunneledSourceServerIDs(ctx, ListTunneledSourceServerIDsParams{OrganizationID: organizationID, SourceID: sourceID})
		if err != nil {
			return nil, fmt.Errorf("list servers of tunneled source %s: %w", sourceID, err)
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("unknown source kind %q", kind)
	}
}

// syncDerivedCopies re-decides the credential header copies on every upstream
// OAuth and EMA client the given servers name, using the backfill's own
// planner so a sync and a run record the same copies. Each client is decided
// from all of its servers' sources, not just the one being written, because
// the rule refusing a client shared across sources reads them all. A client
// whose servers sit on no source carries no copies, so its own are retired.
func syncDerivedCopies(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, serverIDs []uuid.UUID) error {
	if len(serverIDs) == 0 {
		return nil
	}
	oauthClients, err := q.ListOAuthClientsOfServers(ctx, ListOAuthClientsOfServersParams{OrganizationID: opts.OrganizationID, McpServerIds: serverIDs})
	if err != nil {
		return fmt.Errorf("list upstream OAuth clients of written servers: %w", err)
	}
	if len(oauthClients) > 0 {
		rows, err := q.ListOAuthClientSources(ctx, ListOAuthClientSourcesParams{OrganizationID: opts.OrganizationID, OnlyClientIds: oauthClients})
		if err != nil {
			return fmt.Errorf("list sources of written servers' upstream OAuth clients: %w", err)
		}
		if err := copyOntoGroupedOAuthClients(ctx, tx, q, opts, report, rows); err != nil {
			return err
		}
		decided := map[uuid.UUID]bool{}
		for _, row := range rows {
			decided[row.RemoteSessionClientID] = true
		}
		for _, clientID := range oauthClients {
			if decided[clientID] {
				continue
			}
			// No live association on a live server: the client serves nothing,
			// so it carries no source's credentials.
			authClients, err := q.FindOAuthAuthClients(ctx, FindOAuthAuthClientsParams{OrganizationID: opts.OrganizationID, RemoteSessionClientID: clientID})
			if err != nil {
				return fmt.Errorf("find auth clients of upstream OAuth client %s: %w", clientID, err)
			}
			for _, authClientID := range authClients {
				if err := retireClientCopies(ctx, tx, q, opts, report, KindOAuth, authClientID, uuid.NullUUID{UUID: clientID, Valid: true}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}); err != nil {
					return err
				}
			}
		}
	}

	emaClients, err := q.ListEMAClientsOfServers(ctx, ListEMAClientsOfServersParams{OrganizationID: opts.OrganizationID, McpServerIds: serverIDs})
	if err != nil {
		return fmt.Errorf("list ema clients of written servers: %w", err)
	}
	for _, client := range emaClients {
		if len(client.McpServerIds) == 0 {
			// No live link left: as above, it carries no source's credentials.
			if err := retireClientCopies(ctx, tx, q, opts, report, KindEMA, client.AuthClientID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.NullUUID{UUID: client.AuthClientID, Valid: true}); err != nil {
				return err
			}
			continue
		}
		if err := copyOntoEMAClient(ctx, tx, q, opts, report, client.AuthClientID, true, client.McpServerIds); err != nil {
			return err
		}
	}
	return nil
}

// retireClientCopies retires every header copy on one auth client, for a
// client no source's headers reach any more.
func retireClientCopies(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, kind Kind, authClientID uuid.UUID, oauthClientID, onlyOnSource uuid.NullUUID) error {
	if _, _, err := syncHeaders(ctx, tx, q, opts, report, kind, authClientID, true, oauthClientID, onlyOnSource, SourceKindRemote, nil); err != nil {
		return err
	}
	return nil
}

// syncSourceClient decides the one client this source's credential headers
// yield, and records it with its copies and server links.
func syncSourceClient(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, row ListCredentialSourcesRow, report *Report, want *expected) error {
	plan := newEMAPlan()
	if row.AuthorizationHeaders == 0 {
		// Read ownership before deriving a standalone client, including ready
		// bindings with no typed catalog entry. A conflicting scope set or
		// refused issuer must not expose its secrets as a standalone option.
		bindings, err := q.ListEMABackfillBindings(ctx, ListEMABackfillBindingsParams{
			OrganizationID: opts.OrganizationID,
			OnlyProjectID:  uuid.NullUUID{UUID: row.ProjectID, Valid: true},
			OnlyBindingID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		if err != nil {
			return fmt.Errorf("read source EMA ownership: %w", err)
		}
		servers, err := reachedServers(ctx, q, opts.OrganizationID, bindings)
		if err != nil {
			return err
		}
		for _, server := range servers {
			if server.direct {
				plan.served[server.id], plan.direct[server.id] = true, true
			}
		}
	}
	before := report.Outcomes[OutcomeSkippedRace]
	if err := decideSource(ctx, tx, q, opts, row, report, want, plan); err != nil {
		return err
	}
	if report.Outcomes[OutcomeSkippedRace] > before {
		return errors.New("source ownership changed concurrently; retry transaction")
	}
	return nil
}

// retireSourceClients retires every client derived from the source's headers
// that the sync does not keep, with its copies and backfilled links, and the
// backfilled links of a kept client the sync no longer yields.
func retireSourceClients(ctx context.Context, q *Queries, organizationID, kind string, sourceID uuid.UUID, want *expected) error {
	headerIDs, err := sourceHeaderIDs(ctx, q, organizationID, kind, sourceID)
	if err != nil {
		return err
	}
	// Every id this source's headers can derive. A client has no column naming
	// its source, so the ids are the only way back to it; admin-created
	// clients have ids of their own and are never among these.
	types := []string{TypeStaticCredential, TypePassThrough}
	candidates := make([]uuid.UUID, 0, len(types)*(len(headerIDs)+1))
	for _, clientType := range types {
		candidates = append(candidates, SourceClientID(organizationID, kind, sourceID, clientType))
		for _, headerID := range headerIDs {
			candidates = append(candidates, HeaderClientID(organizationID, kind, sourceID, headerID, clientType))
		}
	}

	live, err := q.ListLiveBackfillClients(ctx, ListLiveBackfillClientsParams{OrganizationID: organizationID, AuthClientIds: candidates, ClientTypes: types})
	if err != nil {
		return fmt.Errorf("list derived auth clients of %s source %s: %w", kind, sourceID, err)
	}
	// The backfill's retirement refuses to act on evidence read before a
	// concurrent change. This sync reads under the source's row lock and
	// writes in the same transaction, so its own writes would look like that
	// change; +infinity waives a guard the lock already gives.
	always := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Infinity, Valid: true}
	for _, id := range live {
		if want.clients[id] {
			continue
		}
		if _, err := q.RetireBackfillClient(ctx, RetireBackfillClientParams{ID: id, OrganizationID: organizationID, ClientTypes: types, EvidenceSince: always}); err != nil {
			return fmt.Errorf("retire auth client %s: %w", id, err)
		}
	}

	links, err := q.ListBackfillLinksOfClients(ctx, ListBackfillLinksOfClientsParams{OrganizationID: organizationID, AuthClientIds: candidates})
	if err != nil {
		return fmt.Errorf("list derived server links of %s source %s: %w", kind, sourceID, err)
	}
	for _, link := range links {
		if want.clients[link.AuthClientID] && want.links[linkKey{serverID: link.McpServerID, clientID: link.AuthClientID}] {
			continue
		}
		if _, err := q.RetireBackfillLink(ctx, RetireBackfillLinkParams{ID: link.ID, OrganizationID: organizationID}); err != nil {
			return fmt.Errorf("retire server link %s: %w", link.ID, err)
		}
	}
	return nil
}

// markLinkedServers records that the servers this sync linked a client to need
// upstream authentication, where that is still undecided. It never overwrites
// a decision.
func markLinkedServers(ctx context.Context, q *Queries, organizationID string, want *expected) error {
	linked := make([]uuid.UUID, 0, len(want.links))
	for key := range want.links {
		linked = append(linked, key.serverID)
	}
	if len(linked) == 0 {
		return nil
	}
	undecided, err := q.ListUndecidedUpstreamAuthServers(ctx, ListUndecidedUpstreamAuthServersParams{OrganizationID: organizationID, McpServerIds: linked})
	if err != nil {
		return fmt.Errorf("list servers with undecided upstream auth: %w", err)
	}
	if len(undecided) == 0 {
		return nil
	}
	if _, err := q.MarkServersRequiringUpstreamAuth(ctx, MarkServersRequiringUpstreamAuthParams{OrganizationID: organizationID, McpServerIds: undecided}); err != nil {
		return fmt.Errorf("mark servers requiring upstream auth: %w", err)
	}
	return nil
}

// sourceHeaderIDs is every header ever recorded on one source, deleted ones
// included, so the clients derived from any of them can be found by their
// stable ids.
func sourceHeaderIDs(ctx context.Context, q *Queries, organizationID, kind string, sourceID uuid.UUID) ([]uuid.UUID, error) {
	switch kind {
	case SourceKindRemote:
		ids, err := q.ListRemoteSourceHeaderIDs(ctx, ListRemoteSourceHeaderIDsParams{OrganizationID: organizationID, SourceID: sourceID})
		if err != nil {
			return nil, fmt.Errorf("list headers of remote source %s: %w", sourceID, err)
		}
		return ids, nil
	case SourceKindTunneled:
		ids, err := q.ListTunneledSourceHeaderIDs(ctx, ListTunneledSourceHeaderIDsParams{OrganizationID: organizationID, SourceID: sourceID})
		if err != nil {
			return nil, fmt.Errorf("list headers of tunneled source %s: %w", sourceID, err)
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("unknown source kind %q", kind)
	}
}
