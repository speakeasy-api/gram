package authclientbackfill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SyncEMARegistrations reconciles only the old and new registrations touched
// by a normal writer. Sibling bindings participate in ambiguity decisions;
// unrelated registrations are never migrated. Callers acquire the complete
// source/server scope before changing ownership or taking registration locks.
func SyncEMARegistrations(ctx context.Context, tx pgx.Tx, organizationID string, registrationIDs []uuid.UUID) error {
	if organizationID == "" {
		return errors.New("sync EMA registrations: missing organization")
	}
	registrationIDs = slices.Clone(registrationIDs)
	slices.SortFunc(registrationIDs, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	registrationIDs = slices.DeleteFunc(slices.Compact(registrationIDs), func(id uuid.UUID) bool { return id == uuid.Nil })
	if len(registrationIDs) == 0 {
		return nil
	}
	ctx = withAtomicHeaderSync(ctx)
	q := New(tx)
	if err := q.LockEMALifecycleRegistrations(ctx, LockEMALifecycleRegistrationsParams{OrganizationID: organizationID, RegistrationIds: registrationIDs}); err != nil {
		return err
	}
	rows, err := q.ListEMARegistrationBindings(ctx, ListEMARegistrationBindingsParams{OrganizationID: organizationID, RegistrationIds: registrationIDs})
	if err != nil {
		return fmt.Errorf("read EMA registration bindings: %w", err)
	}
	bindings := make([]ListEMABackfillBindingsRow, len(rows))
	for i, row := range rows {
		bindings[i] = ListEMABackfillBindingsRow(row)
	}
	clients, err := q.ListEMARegistrationClients(ctx, ListEMARegistrationClientsParams{OrganizationID: organizationID, RegistrationIds: registrationIDs})
	if err != nil {
		return err
	}
	if err := q.LockEMALifecycleClients(ctx, LockEMALifecycleClientsParams{OrganizationID: organizationID, AuthClientIds: clients}); err != nil {
		return err
	}
	// Lock links before deciding or copying so a concurrent administrator detach
	// is observed after it commits, never overwritten using a stale snapshot.
	if err := q.LockEMALifecycleLinks(ctx, LockEMALifecycleLinksParams{OrganizationID: organizationID, AuthClientIds: clients}); err != nil {
		return err
	}
	oldLinks, err := q.ListBackfillLinksOfClients(ctx, ListBackfillLinksOfClientsParams{OrganizationID: organizationID, AuthClientIds: clients})
	if err != nil {
		return err
	}
	opts := Options{OrganizationID: organizationID, Apply: true, PageSize: 0}
	report, want, plan := newReport(true), newExpected(), newEMAPlan()
	if err := decideEMABindings(ctx, tx, q, opts, &report, want, plan, bindings); err != nil {
		return err
	}
	if report.Outcomes[OutcomeSkippedRace] > 0 {
		return errors.New("EMA ownership changed concurrently; retry transaction")
	}
	servers := make([]uuid.UUID, 0, len(oldLinks))
	for _, link := range oldLinks {
		servers = append(servers, link.McpServerID)
		if want.raced[link.AuthClientID] || want.links[linkKey{serverID: link.McpServerID, clientID: link.AuthClientID}] {
			continue
		}
		if _, err := q.RetireBackfillLink(ctx, RetireBackfillLinkParams{ID: link.ID, OrganizationID: organizationID}); err != nil {
			return err
		}
	}
	for _, clientID := range clients {
		if want.clients[clientID] {
			continue
		}
		if _, err := q.RetireEMARegistrationClient(ctx, RetireEMARegistrationClientParams{ID: clientID, OrganizationID: organizationID}); err != nil {
			return err
		}
	}
	reached, err := reachedServers(ctx, q, organizationID, bindings)
	if err != nil {
		return err
	}
	for _, server := range reached {
		servers = append(servers, server.id)
	}
	sources, err := q.ListServerCredentialSources(ctx, ListServerCredentialSourcesParams{OrganizationID: organizationID, McpServerIds: servers})
	if err != nil {
		return err
	}
	for _, source := range sources {
		switch source.SourceKind {
		case SourceKindRemote:
			err = SyncRemoteSource(ctx, tx, organizationID, source.SourceID)
		case SourceKindTunneled:
			err = SyncTunneledSource(ctx, tx, organizationID, source.SourceID)
		}
		if err != nil {
			return err
		}
	}
	// Include old servers even after all their EMA links retired, so stale copies
	// cannot survive a retarget or an ownership loss.
	if err := syncDerivedCopies(ctx, tx, q, opts, &report, servers); err != nil {
		return err
	}
	if report.Outcomes[OutcomeSkippedRace] > 0 {
		return errors.New("EMA credential ownership changed concurrently; retry transaction")
	}
	return nil
}

// SyncEMAServers discovers newly reachable registrations as well as old linked
// registrations. It is used after server or gateway reach configuration writes.
func SyncEMAServers(ctx context.Context, tx pgx.Tx, organizationID string, serverIDs []uuid.UUID) error {
	if len(serverIDs) == 0 {
		return nil
	}
	ids, err := New(tx).ListEMAServerRegistrations(ctx, ListEMAServerRegistrationsParams{OrganizationID: organizationID, ServerIds: serverIDs})
	if err != nil {
		return err
	}
	q := New(tx)
	old, err := q.ListEMAFormerServerRegistrations(ctx, ListEMAFormerServerRegistrationsParams{OrganizationID: organizationID, ServerIds: serverIDs})
	if err != nil {
		return err
	}
	affected := map[uuid.UUID]bool{}
	for _, id := range old {
		affected[id] = true
	}
	rows, err := q.ListEMARegistrationBindings(ctx, ListEMARegistrationBindingsParams{OrganizationID: organizationID, RegistrationIds: ids})
	if err != nil {
		return err
	}
	written := map[uuid.UUID]bool{}
	for _, id := range serverIDs {
		written[id] = true
	}
	for _, row := range rows {
		if affected[row.RemoteSessionClientID] {
			continue
		}
		reached, err := reachedServers(ctx, q, organizationID, []ListEMABackfillBindingsRow{ListEMABackfillBindingsRow(row)})
		if err != nil {
			return err
		}
		for _, server := range reached {
			if written[server.id] {
				affected[row.RemoteSessionClientID] = true
				break
			}
		}
	}
	scoped := make([]uuid.UUID, 0, len(affected))
	for _, id := range ids {
		if affected[id] {
			scoped = append(scoped, id)
		}
	}
	return SyncEMARegistrations(ctx, tx, organizationID, scoped)
}

// SyncEMASources reconciles reach after a source URL or protected resource write.
func SyncEMASources(ctx context.Context, tx pgx.Tx, organizationID string, sourceIDs []uuid.UUID) error {
	servers, err := New(tx).ListEMASourceServers(ctx, ListEMASourceServersParams{OrganizationID: organizationID, SourceIds: sourceIDs})
	if err != nil {
		return err
	}
	return SyncEMAServers(ctx, tx, organizationID, servers)
}

// LockEMAWriterScope must precede all source, server and registration mutations.
// Include both the old and prospective projects. Shared registrations expand the
// scope to sibling projects before any row lock is taken. Project advisory
// gates also serialize creation in projects with no sources or servers yet.
// The shared organization reach gate comes first. A transaction also changing
// gateway configuration must take its exclusive gate first, never upgrade.
func LockEMAWriterScope(ctx context.Context, tx pgx.Tx, organizationID string, projectIDs []uuid.UUID) error {
	if err := New(tx).LockEMAGatewayReachReader(ctx, organizationID); err != nil {
		return fmt.Errorf("lock EMA writer gateway reach: %w", err)
	}
	params := ListEMAWriterLockScopeParams{OrganizationID: organizationID, ProjectIds: projectIDs}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("check scope lock context: %w", err)
		}
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin scope lock attempt: %w", err)
		}
		q := New(attempt)
		before, err := q.ListEMAWriterLockScope(ctx, params)
		if err == nil {
			for _, row := range before {
				if row.Kind != "project" {
					continue
				}
				if err = q.LockEMAProjectScope(ctx, LockEMAProjectScopeParams{OrganizationID: organizationID, ProjectID: row.ID}); err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, row := range before {
				if row.Kind == "server" || row.Kind == "project" {
					continue
				}
				if err = lockSource(ctx, attempt, organizationID, row.Kind, row.ID); err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, row := range before {
				if row.Kind != "server" {
					continue
				}
				if err = q.LockEMAPreparationServer(ctx, LockEMAPreparationServerParams{OrganizationID: organizationID, ServerID: row.ID}); err != nil {
					break
				}
			}
		}
		var after []ListEMAWriterLockScopeRow
		if err == nil {
			after, err = q.ListEMAWriterLockScope(ctx, params)
		}
		if err != nil {
			_ = attempt.Rollback(context.WithoutCancel(ctx))
			return err
		}
		if slices.Equal(before, after) {
			if err := attempt.Commit(ctx); err != nil {
				return fmt.Errorf("commit scope locks: %w", err)
			}
			return nil
		}
		if err = attempt.Rollback(ctx); err != nil {
			return fmt.Errorf("rollback scope lock attempt: %w", err)
		}
	}
}

// LockEMAGatewayScope prepares the affected binding projects for a gateway
// configuration write. It does not migrate unrelated organization data.
func LockEMAGatewayScope(ctx context.Context, tx pgx.Tx, organizationID string) ([]uuid.UUID, error) {
	// Exclusive before discovery closes the empty-set/first-binding race.
	// The nested writer scope reacquires shared under this transaction's own
	// exclusive lock; no path upgrades a previously acquired shared lock.
	if err := New(tx).LockEMAGatewayReachWriter(ctx, organizationID); err != nil {
		return nil, fmt.Errorf("lock EMA gateway configuration: %w", err)
	}
	projects, err := New(tx).ListEMAGatewayProjects(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if err = LockEMAWriterScope(ctx, tx, organizationID, projects); err != nil {
		return nil, err
	}
	return New(tx).ListEMAProjectServers(ctx, ListEMAProjectServersParams{OrganizationID: organizationID, ProjectIds: projects})
}
