package usersessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// revocationPushBudget caps the post-commit jti push in
// RevokeUserSessionClient. A healthy Redis answers in well under a
// millisecond, so this covers thousands of sessions; a Redis that is down
// costs on the order of seconds per attempt (1s DialTimeout plus go-redis
// retries, with no short-circuit), so the budget stops the handler from
// blocking for minutes on a client that owns many sessions.
const revocationPushBudget = 5 * time.Second

// maxReportedRevocationFailures caps how many failed jtis are named in the
// error a failed revocation push produces. The counts in the user-facing
// message carry the full picture; this only bounds the diagnostic detail so a
// client owning thousands of sessions can't produce a single enormous log
// record and span attribute.
const maxReportedRevocationFailures = 20

// cimdRefreshCooldown is the server-side floor between metadata refreshes of
// one client. RefreshUserSessionClientCIMD deliberately defeats the document
// cache (purge then unconditional re-read), so the cache no longer bounds
// fetch volume on this path; this comparison against the row's last
// successful read does instead. Sized for the "I just republished my
// document" workflow, where one re-read is the point and a second within
// seconds serves none. Frontend debounce alone would not count -- the
// endpoint is reachable directly.
const cimdRefreshCooldown = 30 * time.Second

// Lists registered clients, DCR and CIMD alike; keyset paginated by id
// (descending). client_secret_hash is stripped from the view.
func (s *Service) ListUserSessionClients(ctx context.Context, payload *gen.ListUserSessionClientsPayload) (*gen.ListUserSessionClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}

	issuerFilter, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, s.logger)
	}

	queries := repo.New(s.db)

	rows, err := queries.ListUserSessionClientsByProjectID(ctx, repo.ListUserSessionClientsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: issuerFilter,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user session clients").LogError(ctx, s.logger)
	}

	clientIDs := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		clientIDs[i] = row.ID
	}

	counts, err := activeSessionCounts(ctx, queries, clientIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count active user sessions").LogError(ctx, s.logger)
	}

	items := make([]*types.UserSessionClient, len(rows))
	for i, row := range rows {
		items[i] = mv.BuildUserSessionClientView(row, counts[row.ID])
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListUserSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// Fetches a client by id. client_secret_hash is stripped from the view.
func (s *Service) GetUserSessionClient(ctx context.Context, payload *gen.GetUserSessionClientPayload) (*types.UserSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client id").LogError(ctx, s.logger)
	}

	queries := repo.New(s.db)

	row, err := queries.GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session client").LogError(ctx, s.logger)
	}

	counts, err := activeSessionCounts(ctx, queries, []uuid.UUID{row.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count active user sessions").LogError(ctx, s.logger)
	}

	return mv.BuildUserSessionClientView(row, counts[row.ID]), nil
}

// activeSessionCounts tallies live sessions per client id. Ids absent from the
// query result hold no active sessions, so the returned map only carries the
// non-zero tallies and a missing key reads as zero.
func activeSessionCounts(ctx context.Context, queries *repo.Queries, clientIDs []uuid.UUID) (map[uuid.UUID]int32, error) {
	counts := make(map[uuid.UUID]int32, len(clientIDs))
	if len(clientIDs) == 0 {
		return counts, nil
	}

	rows, err := queries.CountActiveUserSessionsByClientIDs(ctx, clientIDs)
	if err != nil {
		return nil, fmt.Errorf("count active user sessions by client ids: %w", err)
	}

	for _, row := range rows {
		// user_session_client_id is nullable on user_sessions, but a NULL never
		// matches the id array this query filters on.
		if !row.UserSessionClientID.Valid {
			continue
		}
		counts[row.UserSessionClientID.UUID] = row.ActiveCount
	}

	return counts, nil
}

// Forces a CIMD client's metadata document to be re-read. Purges the stored
// cache state (expiry + ETag) before resolving, so the follow-up read is
// unconditional: back-dating the expiry alone would leave the ETag in place
// and a host answering 304 Not Modified would re-confirm the exact copy the
// operator is trying to stop trusting, with nothing re-fetched or
// re-validated. The purge and its audit entry commit before the fetch, so a
// refresh whose upstream read then fails still leaves the row in the
// "nothing cached, next read is full" state -- which is the safe direction
// for an operator who no longer trusts the cached copy.
func (s *Service) RefreshUserSessionClientCIMD(ctx context.Context, payload *gen.RefreshUserSessionClientCIMDPayload) (*types.UserSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserSessionClientID(id.String()),
	)

	queries := repo.New(s.db)

	// Establishes project ownership and loads the fields the cooldown check
	// and audit entry need, through the same project-scoped read the get
	// endpoint uses. The mutations below carry their own project guard too.
	row, err := queries.GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session client").LogError(ctx, logger)
	}

	if !row.ClientIDMetadataUri.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "client was registered via DCR and has no metadata document to refresh").LogError(ctx, logger)
	}

	// Same flag that gates CIMD resolution on /authorize, evaluated the same
	// way (per organization, no groups). An org whose flag was turned off
	// after CIMD rows were created can still see those rows; it cannot make
	// Gram fetch documents for them.
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagUserSessionCIMD, authCtx.ActiveOrganizationID, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "evaluate user session cimd flag").LogError(ctx, logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeForbidden, nil, "CIMD support is not enabled for this organization").LogError(ctx, logger)
	}

	// fetched_at advances on every successful read (a 304 revalidation
	// included), so this floor only meters clients whose documents are
	// reachable; a host that is down never advances it and every attempt is
	// an upstream fetch. Acceptable: the caller is an authenticated
	// project:write holder re-reading a host their own project points at.
	if row.ClientIDMetadataFetchedAt.Valid && time.Since(row.ClientIDMetadataFetchedAt.Time) < cimdRefreshCooldown {
		return nil, oops.E(oops.CodeRateLimitExceeded, nil, "metadata was read moments ago, try again in a moment")
	}

	// The purge and its audit entry commit together, before the fetch: the
	// purge is the security-relevant mutation, and it must stay recorded even
	// when the re-read that follows fails.
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if _, err := txRepo.PurgeUserSessionClientCIMDCache(ctx, repo.PurgeUserSessionClientCIMDCacheParams{
		ID:        row.ID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row stopped being a live CIMD row between the read above and
			// this write (revoked concurrently).
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "purge cimd metadata cache").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionClientCIMDRefresh(ctx, dbtx, audit.LogUserSessionClientCIMDRefreshEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionClientURN: urn.NewUserSessionClient(row.ID),
		ClientID:             row.ClientID,
		ClientName:           row.ClientName,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log cimd metadata refresh").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Zero cache state forces an unconditional fetch and full re-validation.
	// Runs outside any transaction -- this is a network call against a
	// tenant-chosen host.
	result, err := s.cimdResolver.Resolve(ctx, row.ClientID, cimd.CacheState{ExpiresAt: time.Time{}, ETag: ""})
	if err != nil {
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			// A spec/policy rejection of the document itself, with a
			// client-safe description: the operator's fix is at the document
			// host, not at Gram.
			return nil, oops.E(oops.CodeInvalid, err, "metadata document failed validation: %s; the cached copy was discarded and the document will be re-read on the client's next authorization", oauthErr.Description).LogError(ctx, logger)
		}
		// The message carries the post-purge state because the operator's
		// mental model of a failed refresh is "nothing changed" — here the
		// cache mutation already committed, deliberately (a distrusted copy
		// must not survive a host that refuses the re-read).
		return nil, oops.E(oops.CodeGatewayError, err, "metadata document could not be fetched; the cached copy was discarded and the document will be re-read on the client's next authorization").LogError(ctx, logger)
	}

	// Unreachable with zero cache state: the two cache-hit outcomes require
	// cache state this call did not pass. Checked so a resolver regression
	// cannot make this handler dereference a nil Document.
	if result.Outcome != cimd.CacheOutcomeRefreshed || result.Document == nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("cimd resolver reported %q outcome for an uncached resolve", result.Outcome), "refresh client metadata").LogError(ctx, logger)
	}

	// Persisted through an id-scoped update rather than the authorize path's
	// (issuer, client_id) upsert: the upsert's conflict target is a partial
	// unique index over live rows, so against a row revoked during the fetch
	// it would take the INSERT branch and silently resurrect the client the
	// operator just watched someone revoke.
	fresh, err := queries.UpdateUserSessionClientFromCIMD(ctx, repo.UpdateUserSessionClientFromCIMDParams{
		ID:                   row.ID,
		ProjectID:            *authCtx.ProjectID,
		ClientName:           result.Document.ClientName,
		RedirectUris:         result.Document.RedirectURIs,
		CacheTtlSeconds:      result.TTL.Seconds(),
		ClientIDMetadataEtag: conv.ToPGTextEmpty(result.ETag),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row stopped being an updatable CIMD row between the purge
			// and this write — revoked concurrently, or no longer
			// CIMD-resolved.
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "persist refreshed cimd metadata").LogError(ctx, logger)
	}

	counts, err := activeSessionCounts(ctx, queries, []uuid.UUID{fresh.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count active user sessions").LogError(ctx, logger)
	}

	return mv.BuildUserSessionClientView(fresh, counts[fresh.ID]), nil
}

// Soft-deletes a client registration and cascades to every user_session
// issued through it. Future tokens minted for this client_id are rejected.
func (s *Service) RevokeUserSessionClient(ctx context.Context, payload *gen.RevokeUserSessionClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid client id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeUserSessionClient(ctx, repo.RevokeUserSessionClientParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke user session client").LogError(ctx, logger)
	}

	logger = logger.With(attr.SlogUserSessionClientID(revoked.ID.String()))

	revokedSessions, err := txRepo.SoftDeleteUserSessionsByClientID(ctx, uuid.NullUUID{UUID: revoked.ID, Valid: true})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child user sessions").LogError(ctx, logger)
	}

	// One audit entry per cascaded session, not one covering the batch, so
	// each session's life cycle stays reconstructable and auditlogs.list can
	// filter to a single session id. Emitted before the parent client-revoke
	// event; all of them commit with the cascade. Note this costs two round
	// trips per session (audit row plus outbox append) while the transaction
	// holds row locks on every session it just soft-deleted.
	for _, session := range revokedSessions {
		if err := s.audit.LogUserSessionRevoke(ctx, dbtx, audit.LogUserSessionRevokeEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			UserSessionURN:   urn.NewUserSession(session.ID),
			Principal:        session.SubjectUrn,
			Jti:              session.Jti,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log cascaded user session revocation").LogError(ctx, logger)
		}
	}

	if err := s.audit.LogUserSessionClientRevoke(ctx, dbtx, audit.LogUserSessionClientRevokeEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionClientURN: urn.NewUserSessionClient(revoked.ID),
		ClientID:             revoked.ClientID,
		ClientName:           revoked.ClientName,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session client revocation").LogError(ctx, logger)
	}

	// Tombstone the upstream grants of every subject whose session just
	// cascaded, deduplicated: one client can hold a session per user, and a
	// subject's grants hang off (subject, issuer) rather than off the session,
	// so without this the same grants would be soft-deleted — and revoked
	// upstream — once per session the subject held.
	seenSubjects := make(map[string]struct{}, len(revokedSessions))
	var revokedUpstream []remotesessions.RevokedCredentials
	for _, session := range revokedSessions {
		key := session.SubjectUrn.String() + "\x00" + session.UserSessionIssuerID.String()
		if _, seen := seenSubjects[key]; seen {
			continue
		}
		seenSubjects[key] = struct{}{}

		creds, err := s.revoker.SoftDeleteSubjectSessions(ctx, dbtx, session.SubjectUrn, *authCtx.ProjectID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "revoke upstream remote sessions").LogError(ctx, logger)
		}
		revokedUpstream = append(revokedUpstream, creds...)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Push every cascaded jti into the revocation cache after the DB commit so
	// a cached jti always corresponds to a soft-deleted row. Without this the
	// client's already-issued access tokens keep validating until they expire
	// on their own clock (up to an hour), because the Bearer path checks only
	// signature, expiry, audience, and this cache — never the row's liveness.
	//
	// Bounded: a Redis that is refusing connections costs on the order of a
	// second per attempt, and a blackholed one costs several (1s DialTimeout
	// plus go-redis retries, with no short-circuit). One client row can own a
	// session per user, since a CIMD client_id is shared across every user of
	// that client, so an unbounded loop could block the handler for minutes.
	// pushCtx is passed into each call, so the deadline bounds the loop as a
	// whole rather than each attempt.
	//
	// Detached from the request's cancellation: the rows are already committed
	// as deleted, so a caller that disconnects between the commit and the push
	// would otherwise leave every one of those access tokens live until expiry.
	pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revocationPushBudget)
	defer cancel()

	pushed := 0
	failedJTIs := make([]string, 0, len(revokedSessions))
	var firstErr error
	for _, session := range revokedSessions {
		if pushCtx.Err() != nil {
			break
		}
		if err := s.chatSessions.RevokeToken(pushCtx, session.Jti); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedJTIs = append(failedJTIs, session.Jti)
			continue
		}
		pushed++
	}

	// Strictly after the jti pushes, and attempted even when some failed. This
	// fan-out is synchronous, so running it first would hold every one of the
	// client's already-issued access tokens valid for the length of the batch
	// budget — a slow issuer would delay Gram's own security control. The two
	// are independent, so a cache outage must not also cost the upstream
	// revocations.
	s.revoker.RevokeAllDetached(ctx, revokedUpstream)

	// Surfaced rather than logged and swallowed: the client and its sessions
	// are already gone from the database, so reporting success here would tell
	// an operator that a security control took effect while some access tokens
	// are still live. Marked Permanent because retrying cannot help — the
	// client row is soft-deleted, so a second revoke returns not-found.
	if pushed < len(revokedSessions) {
		cause := firstErr
		if cause == nil {
			cause = pushCtx.Err()
		}
		// Capped: with one session per user, an outage would otherwise put
		// thousands of near-identical jtis into a single log record and span.
		reported := failedJTIs
		if len(reported) > maxReportedRevocationFailures {
			reported = reported[:maxReportedRevocationFailures]
		}
		return oops.E(
			oops.CodeUnexpected,
			oops.Permanent(fmt.Errorf("push revoked jtis (failed, capped at %d: %v): %w", maxReportedRevocationFailures, reported, cause)),
			"revoke access tokens for client (%d of %d sessions invalidated)", pushed, len(revokedSessions),
		).LogError(ctx, logger)
	}

	return nil
}
