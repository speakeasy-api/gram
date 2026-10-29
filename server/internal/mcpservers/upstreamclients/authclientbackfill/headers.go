package authclientbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// idNamespace derives the stable ids of clients that have no natural key, so
// a rerun finds the client an earlier run recorded.
var idNamespace = uuid.MustParse("5d3a8f2e-6b1c-4e7a-9f04-2c8b7d1e6a39")

// HeaderClientID is the stable id of the auth client recorded for a source's
// Authorization header as clientType. A header that switches between a stored
// and a forwarded value yields a new client, and the old one is retired.
func HeaderClientID(organizationID, sourceKind string, sourceID, headerID uuid.UUID, clientType string) uuid.UUID {
	return uuid.NewSHA1(idNamespace, []byte("header:"+organizationID+":"+sourceKind+":"+sourceID.String()+":"+headerID.String()+":"+clientType))
}

// SourceClientID is the stable id of the auth client recorded for a source's
// credential headers as clientType, when the source has no Authorization
// header.
func SourceClientID(organizationID, sourceKind string, sourceID uuid.UUID, clientType string) uuid.UUID {
	return uuid.NewSHA1(idNamespace, []byte("source:"+organizationID+":"+sourceKind+":"+sourceID.String()+":"+clientType))
}

// OAuthClientID is the stable id of the auth client recorded for an upstream
// OAuth client in an organization as clientType, so a retired client returns
// under the same id.
func OAuthClientID(organizationID string, remoteSessionClientID uuid.UUID, clientType string) uuid.UUID {
	return uuid.NewSHA1(idNamespace, []byte("oauth:"+organizationID+":"+remoteSessionClientID.String()+":"+clientType))
}

// EMAClientID is the stable id of the EMA auth client recorded for one
// upstream in an organization, so a retired client returns under the same id.
func EMAClientID(organizationID string, remoteSessionIssuerID, remoteSessionClientID uuid.UUID, resource string) uuid.UUID {
	return uuid.NewSHA1(idNamespace, []byte("ema:"+organizationID+":"+remoteSessionIssuerID.String()+":"+remoteSessionClientID.String()+":"+resource))
}

// Default names for clients whose configuration names none.
const (
	staticCredentialName = "Static credential" // #nosec G101 -- a display name, not a credential
	passThroughName      = "Pass-through"
	emaName              = "Enterprise Managed Authorization"
)

// runHeaders decides every source's credential headers: its Authorization
// header, static headers marked secret, and headers forwarded from the
// request. Other static headers stay server headers, which the runtime keeps
// applying.
//
// A source with an Authorization header gets one static_credential or
// pass_through client, by how that header gets its value, carrying all its
// credential headers and offered by every server on the source, beside any
// upstream OAuth client a server also has. A source without one gets that
// client only for its servers without an upstream OAuth client. The
// non-Authorization credential headers of a server with an upstream OAuth
// client are also copied onto that client's auth client. Copies track their
// source headers: changed values, flags and letter case are updated in place,
// and copies no source header names any more are retired.
func runHeaders(ctx context.Context, tx pgx.Tx, opts Options, pageSize int32, report *Report, want *expected, ema *emaPlan) error {
	q := New(tx)
	cursor := uuid.Nil
	for {
		rows, err := q.ListCredentialSources(ctx, ListCredentialSourcesParams{
			OrganizationID: opts.OrganizationID,
			Cursor:         cursor,
			OnlySourceID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			PageSize:       pageSize,
		})
		if err != nil {
			return fmt.Errorf("list credential header sources after %s: %w", cursor, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if err := decideSource(ctx, tx, q, opts, row, report, want, ema); err != nil {
				return err
			}
			cursor = row.SourceID
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("backfill interrupted: %w", err)
		}
	}
	if err := copyOntoOAuthClients(ctx, tx, q, opts, report); err != nil {
		return err
	}
	return copyOntoEMAClients(ctx, tx, q, opts, report, ema)
}

func decideSource(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, row ListCredentialSourcesRow, report *Report, want *expected, ema *emaPlan) error {
	result := Result{
		Kind:                  KindHeader,
		SubjectID:             row.SourceID,
		AssociationID:         uuid.Nil,
		MCPServerID:           uuid.Nil,
		ProjectID:             row.ProjectID,
		RemoteSessionClientID: uuid.Nil,
		BindingIDs:            nil,
		SourceIDs:             nil,
		ServerIDs:             nil,
		AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Outcome:               OutcomeExisting,
		Reason:                "",
	}
	switch {
	case row.AuthorizationHeaders > 1:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonMultipleAuthorizationHeaders
		report.add(result)
		return nil
	case row.HasDuplicateNames:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonDuplicateHeaderNames
		report.add(result)
		return nil
	case refusesASource(row.ForwardedSources):
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonDeniedSource
		report.add(result)
		return nil
	}

	servers, err := sourceServers(ctx, q, opts.OrganizationID, row.SourceKind, row.SourceID)
	if err != nil {
		return err
	}
	var (
		clientType   string
		id           uuid.UUID
		anchor       = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		requiredAuth = (*bool)(nil)
		linked       = servers
	)
	if row.AuthorizationHeaders == 1 {
		clientType = TypePassThrough
		if row.AuthorizationIsStatic {
			clientType = TypeStaticCredential
		}
		// With no live server to offer it, no client is recorded; an earlier
		// one is retired.
		if len(servers) == 0 {
			return nil
		}
		anchor = uuid.NullUUID{UUID: row.AuthorizationHeaderID, Valid: true}
		id = HeaderClientID(opts.OrganizationID, row.SourceKind, row.SourceID, row.AuthorizationHeaderID, clientType)
	} else {
		// Without an Authorization header, a server with an upstream OAuth
		// client or an EMA client serving its direct route gets the credential
		// headers on that client instead. One reached only through the
		// gateways keeps this client for its direct route.
		linked = nil
		for _, server := range servers {
			if !server.hasOAuth && !ema.reachesDirectly(server.id) {
				linked = append(linked, server)
			}
		}
		if len(linked) == 0 {
			return nil
		}
		switch {
		case row.AllStatic:
			clientType = TypeStaticCredential
		case row.AllForwarded:
			clientType = TypePassThrough
		default:
			result.Outcome, result.Reason = OutcomeAmbiguous, ReasonMixedCredentialHeaders
			report.add(result)
			return nil
		}
		id = SourceClientID(opts.OrganizationID, row.SourceKind, row.SourceID, clientType)
		noOAuth := false
		requiredAuth = &noOAuth
	}
	result.AuthClientID = uuid.NullUUID{UUID: id, Valid: true}
	want.clients[id] = true

	recorded, live, err := clientState(ctx, q, opts.OrganizationID, id, clientType)
	if err != nil {
		return err
	}
	if !live {
		result.Outcome = OutcomeCreated
		report.pendingClient(KindHeader)
		if opts.Apply {
			name := passThroughName
			if clientType == TypeStaticCredential {
				name = staticCredentialName
			}
			n, err := q.CreateSourceAuthClient(ctx, CreateSourceAuthClientParams{
				ID:             id,
				OrganizationID: opts.OrganizationID,
				ClientType:     clientType,
				Name:           name,
				SourceKind:     row.SourceKind,
				SourceID:       row.SourceID,
				AnchorHeaderID: anchor,
			})
			if err != nil {
				return fmt.Errorf("record auth client for source %s: %w", row.SourceID, err)
			}
			if n == 0 {
				result.Outcome = OutcomeSkippedRace
				want.race(id)
				report.add(result)
				return nil
			}
			report.wroteClient(KindHeader)
			recorded = true
		}
	}

	// A retired client is revived, so its surviving copies and links count.
	changed, raced, err := syncHeaders(ctx, tx, q, opts, report, KindHeader, id, recorded, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, row.SourceKind, row.HeaderIds)
	if err != nil {
		return err
	}
	switch {
	case raced:
		result.Outcome = OutcomeSkippedRace
	case changed && result.Outcome == OutcomeExisting:
		result.Outcome = OutcomeUpdated
	}
	report.add(result)
	if raced {
		want.race(id)
		return nil
	}

	for _, server := range linked {
		link := Result{
			Kind:                  KindHeader,
			SubjectID:             row.SourceID,
			AssociationID:         uuid.Nil,
			MCPServerID:           server.id,
			ProjectID:             server.projectID,
			RemoteSessionClientID: uuid.Nil,
			BindingIDs:            nil,
			SourceIDs:             nil,
			ServerIDs:             nil,
			AuthClientID:          uuid.NullUUID{UUID: id, Valid: true},
			Outcome:               OutcomeExisting,
			Reason:                "",
		}
		err := linkServer(ctx, q, opts, &link, recorded, report, want, func() (int64, error) {
			params := LinkSourceServerParams{
				OrganizationID:   opts.OrganizationID,
				AuthClientID:     id,
				McpServerID:      server.id,
				SourceKind:       row.SourceKind,
				SourceID:         row.SourceID,
				RequiredHasOauth: nullBool(requiredAuth),
			}
			return q.LinkSourceServer(ctx, params)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// sourceServer is one live server on a source.
type sourceServer struct {
	id        uuid.UUID
	projectID uuid.UUID
	hasOAuth  bool
}

func sourceServers(ctx context.Context, q *Queries, organizationID, sourceKind string, sourceID uuid.UUID) ([]sourceServer, error) {
	var out []sourceServer
	switch sourceKind {
	case "remote":
		rows, err := q.ListRemoteSourceServers(ctx, ListRemoteSourceServersParams{OrganizationID: organizationID, SourceID: sourceID})
		if err != nil {
			return nil, fmt.Errorf("list servers of remote source %s: %w", sourceID, err)
		}
		for _, r := range rows {
			out = append(out, sourceServer{id: r.McpServerID, projectID: r.ProjectID, hasOAuth: r.HasOauth})
		}
	case "tunneled":
		rows, err := q.ListTunneledSourceServers(ctx, ListTunneledSourceServersParams{OrganizationID: organizationID, SourceID: sourceID})
		if err != nil {
			return nil, fmt.Errorf("list servers of tunneled source %s: %w", sourceID, err)
		}
		for _, r := range rows {
			out = append(out, sourceServer{id: r.McpServerID, projectID: r.ProjectID, hasOAuth: r.HasOauth})
		}
	default:
		return nil, fmt.Errorf("unknown source kind %q", sourceKind)
	}
	return out, nil
}

// copyOntoOAuthClients copies the non-Authorization credential headers of a
// source onto the auth clients of the upstream OAuth client its servers use,
// only when every server using that upstream OAuth client sits on that one
// source; otherwise one source's credentials would be sent from the others'
// servers. Such a shared upstream OAuth client is reported, never copied
// onto, and any copies on it are retired, as are copies no source names any
// more.
func copyOntoOAuthClients(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report) error {
	rows, err := q.ListOAuthClientSources(ctx, ListOAuthClientSourcesParams{
		OrganizationID: opts.OrganizationID,
		OnlyClientIds:  nil,
	})
	if err != nil {
		return fmt.Errorf("list upstream OAuth client sources: %w", err)
	}
	return copyOntoGroupedOAuthClients(ctx, tx, q, opts, report, rows)
}

// copyOntoGroupedOAuthClients decides each upstream OAuth client named by
// rows. rows must carry every source of each client it names, since the rule
// refusing a client shared across sources reads them all.
func copyOntoGroupedOAuthClients(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, rows []ListOAuthClientSourcesRow) error {
	var order []uuid.UUID
	sources := map[uuid.UUID][]ListOAuthClientSourcesRow{}
	for _, row := range rows {
		if _, ok := sources[row.RemoteSessionClientID]; !ok {
			order = append(order, row.RemoteSessionClientID)
		}
		sources[row.RemoteSessionClientID] = append(sources[row.RemoteSessionClientID], row)
	}
	for _, clientID := range order {
		if err := copyOntoOAuthClient(ctx, tx, q, opts, report, clientID, sources[clientID]); err != nil {
			return err
		}
	}
	return nil
}

// copyOntoOAuthClient decides the credential header copies on one upstream
// OAuth client's auth clients from the sources its servers sit on.
func copyOntoOAuthClient(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, clientID uuid.UUID, used []ListOAuthClientSourcesRow) error {
	if len(used) == 0 {
		return nil
	}
	result := Result{
		Kind:                  KindOAuth,
		SubjectID:             clientID,
		AssociationID:         uuid.Nil,
		MCPServerID:           uuid.Nil,
		ProjectID:             uuid.Nil,
		RemoteSessionClientID: clientID,
		BindingIDs:            nil,
		SourceIDs:             nil,
		ServerIDs:             nil,
		AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Outcome:               OutcomeExisting,
		Reason:                "",
	}
	withHeaders := false
	for _, src := range used {
		withHeaders = withHeaders || len(src.HeaderIds) > 0
	}
	srcKind, desired := "remote", []uuid.UUID(nil)
	shared := len(used) > 1 && withHeaders
	switch {
	case shared:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonOAuthClientSharedAcrossSources
		for _, src := range used {
			result.SourceIDs = append(result.SourceIDs, src.SourceID)
			result.ServerIDs = append(result.ServerIDs, src.ServerIds...)
		}
	case len(used) == 1 && used[0].HasDuplicateNames:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonDuplicateHeaderNames
		result.SourceIDs = []uuid.UUID{used[0].SourceID}
		result.ServerIDs = used[0].ServerIds
	case len(used) == 1 && refusesASource(used[0].ForwardedSources):
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonDeniedSource
		result.SourceIDs = []uuid.UUID{used[0].SourceID}
		result.ServerIDs = used[0].ServerIds
	case len(used) == 1:
		srcKind, desired = used[0].SourceKind, used[0].HeaderIds
	}
	ambiguous := result.Outcome == OutcomeAmbiguous
	authClients, err := q.FindOAuthAuthClients(ctx, FindOAuthAuthClientsParams{OrganizationID: opts.OrganizationID, RemoteSessionClientID: clientID})
	if err != nil {
		return fmt.Errorf("find auth clients of upstream OAuth client %s: %w", clientID, err)
	}
	if len(authClients) == 0 {
		// Recorded by the OAuth phase on apply; a dry run counts the copies
		// of a client that phase would record.
		_, recordable := Type(used[0].CredentialOwner)
		switch {
		case ambiguous:
			report.add(result)
		case len(desired) > 0 && !opts.Apply && recordable:
			result.Outcome = OutcomeUpdated
			for range desired {
				report.pendingHeader(KindOAuth)
			}
			report.add(result)
		}
		return nil
	}
	changedAny, racedAny := false, false
	for _, authClientID := range authClients {
		changed, raced, err := syncHeaders(ctx, tx, q, opts, report, KindOAuth, authClientID, true, uuid.NullUUID{UUID: clientID, Valid: true}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, srcKind, desired)
		if err != nil {
			return err
		}
		changedAny = changedAny || changed
		racedAny = racedAny || raced
		result.AuthClientID = uuid.NullUUID{UUID: authClientID, Valid: true}
	}
	switch {
	case ambiguous:
	case racedAny:
		result.Outcome = OutcomeSkippedRace
	case changedAny:
		result.Outcome = OutcomeUpdated
	case len(desired) == 0:
		return nil
	}
	report.add(result)
	return nil
}

// copyOntoEMAClients copies the non-Authorization credential headers of the
// source an ema client's servers sit on onto that client, only when they all
// sit on that one source, as copyOntoOAuthClients does for upstream OAuth
// clients. An ema client shared across sources with headers is reported,
// never copied onto, and any copies on it are retired.
func copyOntoEMAClients(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, ema *emaPlan) error {
	if ema == nil {
		return nil
	}
	for _, target := range ema.targets {
		if len(target.servers) == 0 {
			continue
		}
		if err := copyOntoEMAClient(ctx, tx, q, opts, report, target.clientID, target.recorded, target.servers); err != nil {
			return err
		}
	}
	return nil
}

// copyOntoEMAClient decides the credential header copies on one ema auth
// client from the sources of every server that offers it. servers must be all
// of them, since the rule refusing a client shared across sources reads them
// all.
func copyOntoEMAClient(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, clientID uuid.UUID, recorded bool, servers []uuid.UUID) error {
	if opts.Apply && recorded {
		if _, err := q.LockEMARegistrationForSync(ctx, LockEMARegistrationForSyncParams{
			OrganizationID: opts.OrganizationID,
			AuthClientID:   uuid.NullUUID{UUID: clientID, Valid: true},
		}); err != nil {
			return fmt.Errorf("lock EMA credential registration: %w", err)
		}
		if err := q.LockEMACopyClient(ctx, LockEMACopyClientParams{
			OrganizationID: opts.OrganizationID, AuthClientID: clientID,
		}); err != nil {
			return fmt.Errorf("lock EMA credential client: %w", err)
		}
		// Serialize link retirement/detach before rereading the owner set. A
		// registration lock alone does not protect administrator link mutations.
		if err := q.LockEMALifecycleLinks(ctx, LockEMALifecycleLinksParams{
			OrganizationID: opts.OrganizationID, AuthClientIds: []uuid.UUID{clientID},
		}); err != nil {
			return fmt.Errorf("lock EMA credential links: %w", err)
		}
		// The caller's link snapshot can precede a concurrent binding writer.
		var err error
		servers, err = q.ListEMAClientLiveServers(ctx, ListEMAClientLiveServersParams{
			OrganizationID: opts.OrganizationID, AuthClientID: clientID,
		})
		if err != nil {
			return fmt.Errorf("reload EMA credential owners: %w", err)
		}
		if len(servers) == 0 {
			return retireClientCopies(ctx, tx, q, opts, report, KindEMA, clientID,
				uuid.NullUUID{}, uuid.NullUUID{UUID: clientID, Valid: true})
		}
	}
	used, err := q.ListServerCredentialSources(ctx, ListServerCredentialSourcesParams{OrganizationID: opts.OrganizationID, McpServerIds: servers})
	if err != nil {
		return fmt.Errorf("list credential sources of ema auth client %s: %w", clientID, err)
	}
	if len(used) == 0 {
		return nil
	}
	result := Result{
		Kind:                  KindEMA,
		SubjectID:             clientID,
		AssociationID:         uuid.Nil,
		MCPServerID:           uuid.Nil,
		ProjectID:             uuid.Nil,
		RemoteSessionClientID: uuid.Nil,
		BindingIDs:            nil,
		SourceIDs:             nil,
		ServerIDs:             nil,
		AuthClientID:          uuid.NullUUID{UUID: clientID, Valid: clientID != uuid.Nil},
		Outcome:               OutcomeExisting,
		Reason:                "",
	}
	withHeaders := false
	for _, src := range used {
		withHeaders = withHeaders || len(src.HeaderIds) > 0
	}
	srcKind, desired := "remote", []uuid.UUID(nil)
	switch {
	case len(used) > 1 && withHeaders:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonEMAClientSharedAcrossSources
		for _, src := range used {
			result.SourceIDs = append(result.SourceIDs, src.SourceID)
			result.ServerIDs = append(result.ServerIDs, src.ServerIds...)
		}
	case len(used) == 1 && used[0].HasDuplicateNames:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonDuplicateHeaderNames
		result.SourceIDs, result.ServerIDs = []uuid.UUID{used[0].SourceID}, used[0].ServerIds
	case len(used) == 1 && refusesASource(used[0].ForwardedSources):
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonDeniedSource
		result.SourceIDs, result.ServerIDs = []uuid.UUID{used[0].SourceID}, used[0].ServerIds
	case len(used) == 1:
		srcKind, desired = used[0].SourceKind, used[0].HeaderIds
	}
	ambiguous := result.Outcome == OutcomeAmbiguous
	changed, raced := false, false
	if clientID != uuid.Nil || len(desired) > 0 {
		// A client a dry run would record has no copies yet: they all count
		// as pending.
		changed, raced, err = syncHeaders(ctx, tx, q, opts, report, KindEMA, clientID, recorded, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.NullUUID{UUID: clientID, Valid: clientID != uuid.Nil}, srcKind, desired)
		if err != nil {
			return err
		}
	}
	switch {
	case ambiguous:
	case raced:
		result.Outcome = OutcomeSkippedRace
	case changed:
		result.Outcome = OutcomeUpdated
	default:
		return nil
	}
	report.add(result)
	return nil
}

type atomicHeaderSyncKey struct{}

// withAtomicHeaderSync makes normal writers roll back when a copy write loses
// its ownership evidence. Backfill retains its report-and-retry behavior.
func withAtomicHeaderSync(ctx context.Context) context.Context {
	return context.WithValue(ctx, atomicHeaderSyncKey{}, true)
}

// syncHeaders brings an auth client's header copies in line with the given
// source headers: missing ones are copied, changed ones updated in place, and
// copies no source header names, or case-variant extras, retired. A client
// not recorded yet counts every source header as pending. Copies onto an
// upstream OAuth client's auth client, named by oauthClientID, are written
// only while every server using that client sits on the headers' source.
// changed reports pending work; raced reports a write whose evidence changed.
func syncHeaders(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, report *Report, kind Kind, authClientID uuid.UUID, recorded bool, oauthClientID, onlyOnSource uuid.NullUUID, sourceKind string, headerIDs []uuid.UUID) (changed, raced bool, err error) {
	if !recorded {
		for range headerIDs {
			report.pendingHeader(kind)
		}
		return len(headerIDs) > 0, false, nil
	}
	remote, tunneled := []uuid.UUID{}, []uuid.UUID{}
	if sourceKind == "tunneled" {
		tunneled = headerIDs
	} else {
		remote = headerIDs
	}
	plan, err := q.PlanClientHeaders(ctx, PlanClientHeadersParams{
		OrganizationID:    opts.OrganizationID,
		RemoteHeaderIds:   remote,
		TunneledHeaderIds: tunneled,
		AuthClientID:      authClientID,
	})
	if err != nil {
		return false, false, fmt.Errorf("plan header copies of auth client %s: %w", authClientID, err)
	}
	for _, step := range plan {
		var write func() (int64, error)
		switch {
		case step.SrcID == uuid.Nil:
			write = func() (int64, error) {
				return q.RetireHeaderCopy(ctx, RetireHeaderCopyParams{CopyID: step.CopyID, OrganizationID: opts.OrganizationID})
			}
		case step.CopyID == uuid.Nil:
			write = func() (int64, error) {
				var n int64
				err := inSavepoint(ctx, tx, func(sp pgx.Tx) error {
					var err error
					n, err = New(sp).InsertHeaderCopy(ctx, InsertHeaderCopyParams{
						SrcKind:                       step.SrcKind,
						SrcID:                         step.SrcID,
						OrganizationID:                opts.OrganizationID,
						AuthClientID:                  authClientID,
						RequiredRemoteSessionClientID: oauthClientID,
						RequiredAuthClientID:          onlyOnSource,
					})
					return err
				})
				if isUniqueViolation(err) {
					return 0, nil
				}
				return n, err
			}
		case step.Differs:
			write = func() (int64, error) {
				return q.UpdateHeaderCopy(ctx, UpdateHeaderCopyParams{
					SrcKind:                       step.SrcKind,
					SrcID:                         step.SrcID,
					CopyID:                        step.CopyID,
					OrganizationID:                opts.OrganizationID,
					RequiredRemoteSessionClientID: oauthClientID,
					RequiredAuthClientID:          onlyOnSource,
				})
			}
		default:
			continue
		}
		changed = true
		report.pendingHeader(kind)
		if !opts.Apply {
			continue
		}
		n, err := write()
		if err != nil {
			return changed, raced, fmt.Errorf("sync a header copy of auth client %s: %w", authClientID, err)
		}
		if n == 0 {
			if atomic, _ := ctx.Value(atomicHeaderSyncKey{}).(bool); atomic {
				return changed, true, fmt.Errorf("credential copy ownership changed concurrently; retry transaction")
			}
			raced = true
			continue
		}
		report.wroteHeader(kind)
	}
	return changed, raced, nil
}

// clientState reports whether the backfilled client with this stable id is
// recorded, deleted or not, and whether it is live. A deleted one is revived
// by its create; a live one of another type cannot exist, since the id
// derives from the type.
func clientState(ctx context.Context, q *Queries, organizationID string, id uuid.UUID, clientType string) (recorded, live bool, err error) {
	state, err := q.GetAuthClientState(ctx, GetAuthClientStateParams{ID: id, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, false, nil
	case err != nil:
		return false, false, fmt.Errorf("read auth client %s: %w", id, err)
	case !state.Deleted && state.ClientType != clientType:
		return false, false, fmt.Errorf("auth client %s is a %s client, not %s", id, state.ClientType, clientType)
	}
	return true, !state.Deleted, nil
}

// linkServer decides one server's link to an auth client and, with Apply,
// records it with write, which repeats the deciding predicate. recorded
// reports that the client exists, so a dry run over a client not yet recorded
// counts the link as pending without reading it.
func linkServer(ctx context.Context, q *Queries, opts Options, link *Result, recorded bool, report *Report, want *expected, write func() (int64, error)) error {
	clientID := link.AuthClientID.UUID
	want.links[linkKey{serverID: link.MCPServerID, clientID: clientID}] = true
	linked := false
	if recorded {
		var err error
		linked, err = q.IsServerLinked(ctx, IsServerLinkedParams{McpServerID: link.MCPServerID, AuthClientID: clientID, OrganizationID: opts.OrganizationID})
		if err != nil {
			return fmt.Errorf("read link of server %s: %w", link.MCPServerID, err)
		}
	}
	if linked {
		report.add(*link)
		return nil
	}
	link.Outcome = OutcomeAssociated
	report.pendingLink(link.Kind)
	if !opts.Apply {
		report.add(*link)
		return nil
	}
	n, err := write()
	if err != nil {
		return fmt.Errorf("link server %s to auth client %s: %w", link.MCPServerID, clientID, err)
	}
	if n == 0 {
		link.Outcome = OutcomeSkippedRace
	} else {
		report.wroteLink(link.Kind)
	}
	report.add(*link)
	return nil
}

// nullBool converts an optional bool to its query parameter.
func nullBool(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{Bool: false, Valid: false}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

// refusesASource reports whether the proxy refuses any of these request
// headers as the source of a forwarded header, as proxy.CheckRemoteHeader
// does: Authorization and the other denied sources, and every protected
// inbound header.
func refusesASource(sources []string) bool {
	for _, src := range sources {
		if proxy.IsDeniedPassThroughSource(src) || proxy.IsProtectedInboundHeader(src) {
			return true
		}
	}
	return false
}
