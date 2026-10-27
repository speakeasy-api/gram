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

// SourceKindRemote names a remote MCP source.
const SourceKindRemote = "remote"

// ErrSourceNotFound reports a source outside the organization.
var ErrSourceNotFound = errors.New("credential source not found in organization")

// LockRemoteSource takes the row lock SyncRemoteSource needs, so a caller that
// locks one of the source's MCP servers can take it first and keep every
// path's lock order source-then-server. SyncRemoteSource takes the same lock,
// so a caller that locks no server need not call this. ErrSourceNotFound means
// the source is not the organization's.
func LockRemoteSource(ctx context.Context, tx pgx.Tx, organizationID string, sourceID uuid.UUID) error {
	if organizationID == "" || sourceID == uuid.Nil {
		return errors.New("lock remote credential source: no tenant scope")
	}
	if _, err := New(tx).LockRemoteCredentialSource(ctx, LockRemoteCredentialSourceParams{OrganizationID: organizationID, SourceID: sourceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSourceNotFound
		}
		return fmt.Errorf("lock remote source %s: %w", sourceID, err)
	}
	return nil
}

// SyncRemoteSource brings the auth clients derived from one remote source's
// credential headers in line with its headers and live servers, inside the
// caller's transaction, as the header and server writes that change them
// happen. It makes the same decisions as the backfill for that source, so a
// header set up today gets its typed client without waiting for the next
// backfill run:
//
//   - A source with one Authorization header and live servers gets its
//     static_credential or pass_through client under the stable id, carrying
//     all its credential headers as copies and offered by each live server on
//     the source, unless an administrator detached it from that server.
//   - A source without an Authorization header keeps a client the backfill
//     already recorded for it, with its copies kept current; creating one
//     depends on the organization's upstream OAuth and EMA clients, so it is
//     left to the backfill.
//   - Every other client derived from the source's headers is retired with its
//     copies and backfilled links, as are backfilled links from servers no
//     longer on the source.
//
// Newly linked servers whose need for upstream authentication is undecided
// are marked as needing it. Grants, credential choices, administrator clients
// and administrator links are left alone, as are the credential headers
// copied onto an upstream OAuth or EMA client, which the backfill owns. A
// decision whose evidence changed during the sync is an error, so the caller's
// write is rolled back rather than left without its clients.
//
// This takes the source row lock, which every writer of a source takes before
// touching its headers or its servers. A caller that locks one of the source's
// MCP servers first must call LockRemoteSource before that lock, so the two
// never wait on each other in opposite order.
func SyncRemoteSource(ctx context.Context, tx pgx.Tx, organizationID string, sourceID uuid.UUID) error {
	if err := LockRemoteSource(ctx, tx, organizationID, sourceID); err != nil {
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
		return fmt.Errorf("read credential headers of remote source %s: %w", sourceID, err)
	}
	// No row means the source, its project or its last credential header is
	// gone: nothing is wanted and everything derived from it is retired below.
	if len(rows) == 1 {
		row := rows[0]
		if row.SourceKind != SourceKindRemote {
			return fmt.Errorf("sync auth clients of remote source %s: read a %s source", sourceID, row.SourceKind)
		}
		if err := syncSourceClient(ctx, tx, q, opts, row, &report, want); err != nil {
			return err
		}
	}
	if len(want.raced) > 0 {
		return fmt.Errorf("sync auth clients of remote source %s: configuration changed concurrently", sourceID)
	}

	if err := retireSourceClients(ctx, q, organizationID, sourceID, want); err != nil {
		return err
	}

	return markLinkedServers(ctx, q, organizationID, want)
}

// syncSourceClient decides the one client this source's credential headers
// yield, and records it with its copies and server links.
func syncSourceClient(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, row ListCredentialSourcesRow, report *Report, want *expected) error {
	// With an Authorization header the decision does not depend on the
	// organization's upstream OAuth or EMA clients, so the backfill's own
	// derivation decides it with no EMA plan.
	if row.AuthorizationHeaders == 1 {
		return decideSource(ctx, tx, q, opts, row, report, want, nil)
	}
	// Without one, which servers get the client depends on their upstream
	// OAuth and EMA clients, which this sync does not read. An existing client
	// keeps its copies and links current; recording a new one waits for the
	// backfill. Every other shape — several Authorization headers, names
	// differing only in letter case, a header forwarded from a refused request
	// header, or a mix of stored and forwarded values — yields no client here
	// just as it yields none in the backfill.
	if row.AuthorizationHeaders != 0 || row.HasDuplicateNames || refusesASource(row.ForwardedSources) {
		return nil
	}
	clientType := TypePassThrough
	switch {
	case row.AllStatic:
		clientType = TypeStaticCredential
	case !row.AllForwarded:
		return nil
	}
	id := SourceClientID(opts.OrganizationID, SourceKindRemote, row.SourceID, clientType)
	_, live, err := clientState(ctx, q, opts.OrganizationID, id, clientType)
	if err != nil {
		return err
	}
	if !live {
		return nil
	}
	want.clients[id] = true
	noAnchor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	_, raced, err := syncHeaders(ctx, tx, q, opts, report, KindHeader, id, true, noAnchor, noAnchor, SourceKindRemote, row.HeaderIds)
	if err != nil {
		return err
	}
	if raced {
		want.race(id)
		return nil
	}
	// Which servers offer it is the backfill's to decide, so its links stand.
	links, err := q.ListBackfillLinksOfClients(ctx, ListBackfillLinksOfClientsParams{OrganizationID: opts.OrganizationID, AuthClientIds: []uuid.UUID{id}})
	if err != nil {
		return fmt.Errorf("list links of auth client %s: %w", id, err)
	}
	for _, link := range links {
		want.links[linkKey{serverID: link.McpServerID, clientID: id}] = true
	}
	return nil
}

// retireSourceClients retires every client derived from the source's headers
// that the sync does not keep, with its copies and backfilled links, and the
// backfilled links of a kept client the sync no longer yields.
func retireSourceClients(ctx context.Context, q *Queries, organizationID string, sourceID uuid.UUID, want *expected) error {
	headerIDs, err := q.ListRemoteSourceHeaderIDs(ctx, ListRemoteSourceHeaderIDsParams{OrganizationID: organizationID, SourceID: sourceID})
	if err != nil {
		return fmt.Errorf("list headers of remote source %s: %w", sourceID, err)
	}
	// Every id this source's headers can derive. A client has no column naming
	// its source, so the ids are the only way back to it; admin-created
	// clients have ids of their own and are never among these.
	types := []string{TypeStaticCredential, TypePassThrough}
	candidates := make([]uuid.UUID, 0, len(types)*(len(headerIDs)+1))
	for _, clientType := range types {
		candidates = append(candidates, SourceClientID(organizationID, SourceKindRemote, sourceID, clientType))
		for _, headerID := range headerIDs {
			candidates = append(candidates, HeaderClientID(organizationID, SourceKindRemote, sourceID, headerID, clientType))
		}
	}

	live, err := q.ListLiveBackfillClients(ctx, ListLiveBackfillClientsParams{OrganizationID: organizationID, AuthClientIds: candidates, ClientTypes: types})
	if err != nil {
		return fmt.Errorf("list derived auth clients of remote source %s: %w", sourceID, err)
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
		return fmt.Errorf("list derived server links of remote source %s: %w", sourceID, err)
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
