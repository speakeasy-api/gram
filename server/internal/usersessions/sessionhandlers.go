package usersessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// loadUpstreamsForSessions resolves the outbound leg for a page of sessions in
// one query, keyed by the (subject_urn, user_session_issuer_id) pair both
// tables carry. One round trip for the page rather than one per row: a page of
// 50 sessions would otherwise be 51 queries.
//
// Distinct pairs are collected first because a subject commonly holds several
// sessions against the same issuer — one per MCP client — and every one of them
// shares the same upstream tokens.
func (s *Service) loadUpstreamsForSessions(ctx context.Context, projectID uuid.UUID, rows []repo.ListUserSessionsByProjectIDRow) (map[mv.UpstreamKey][]*types.UserSessionUpstream, error) {
	if len(rows) == 0 {
		return map[mv.UpstreamKey][]*types.UserSessionUpstream{}, nil
	}

	seen := make(map[mv.UpstreamKey]struct{}, len(rows))
	subjectURNs := make([]string, 0, len(rows))
	issuerIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		key := mv.UpstreamKey{
			SubjectURN:          row.SubjectUrn.String(),
			UserSessionIssuerID: row.UserSessionIssuerID,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		subjectURNs = append(subjectURNs, key.SubjectURN)
		issuerIDs = append(issuerIDs, key.UserSessionIssuerID)
	}

	upstreamRows, err := repo.New(s.db).ListRemoteSessionUpstreamsForSubjects(ctx, repo.ListRemoteSessionUpstreamsForSubjectsParams{
		SubjectUrns: subjectURNs,
		IssuerIds:   issuerIDs,
		// Scopes on the user_session_issuer's project rather than the client's,
		// so an upstream held through an organization-level or global client
		// still surfaces here. See the query for why.
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session upstreams").LogError(ctx, s.logger)
	}

	return mv.BuildUserSessionUpstreamIndex(upstreamRows), nil
}

// Lists issued sessions; keyset paginated by id (descending).
// refresh_token_hash is excluded from the projection.
func (s *Service) ListUserSessions(ctx context.Context, payload *gen.ListUserSessionsPayload) (*gen.ListUserSessionsResult, error) {
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

	clientFilter, err := conv.PtrToNullUUID(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_id").LogError(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		Status:              conv.PtrToPGTextEmpty(payload.Status),
		SubjectUrn:          conv.PtrToPGTextEmpty(payload.SubjectUrn),
		UserSessionIssuerID: issuerFilter,
		ClientID:            clientFilter,
		ID:                  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user sessions").LogError(ctx, s.logger)
	}

	upstreams, err := s.loadUpstreamsForSessions(ctx, *authCtx.ProjectID, rows)
	if err != nil {
		return nil, err
	}

	items := mv.BuildUserSessionListView(rows, upstreams)

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListUserSessionsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// ListFacets returns available facet values for the user session feed filters.
func (s *Service) ListFacets(ctx context.Context, _ *gen.ListFacetsPayload) (*gen.ListUserSessionFacetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	q := repo.New(s.db)
	clients, err := q.ListUserSessionClientFacets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list client facets").LogError(ctx, s.logger)
	}
	users, err := q.ListUserSessionUserFacets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user facets").LogError(ctx, s.logger)
	}
	servers, err := q.ListUserSessionServerFacets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server facets").LogError(ctx, s.logger)
	}

	clientOpts := make([]*gen.UserSessionFacetOption, len(clients))
	for i, r := range clients {
		clientOpts[i] = &gen.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	userOpts := make([]*gen.UserSessionFacetOption, len(users))
	for i, r := range users {
		userOpts[i] = &gen.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	serverOpts := make([]*gen.UserSessionFacetOption, len(servers))
	for i, r := range servers {
		serverOpts[i] = &gen.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}

	return &gen.ListUserSessionFacetsResult{
		Clients: clientOpts,
		Users:   userOpts,
		Servers: serverOpts,
	}, nil
}

// Soft-deletes the session and pushes its jti into the revocation cache
// so the access token stops validating before its TTL expires.
func (s *Service) RevokeUserSession(ctx context.Context, payload *gen.RevokeUserSessionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid session id").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeUserSession(ctx, repo.RevokeUserSessionParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke user session").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionRevoke(ctx, dbtx, audit.LogUserSessionRevokeEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		UserSessionURN:   urn.NewUserSession(revoked.ID),
		Principal:        revoked.SubjectUrn,
		Jti:              revoked.Jti,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session revocation").LogError(ctx, logger)
	}

	// Tombstone the subject's upstream grants in the same transaction; the
	// RFC 7009 pushes wait until it commits.
	revokedUpstream, err := s.revoker.SoftDeleteSubjectSessions(ctx, dbtx, revoked.SubjectUrn, revoked.UserSessionIssuerID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke upstream remote sessions").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Push the jti into the revocation cache after the DB commit so a cached
	// jti always corresponds to a soft-deleted row. Cache-write failure is
	// surfaced as Unexpected — the row is gone but the access token would keep
	// validating until expiry, which is the case the cache exists to prevent.
	pushErr := s.chatSessions.RevokeToken(ctx, revoked.Jti)

	// Strictly after the jti push, and attempted even when it failed. The
	// upstream call is a synchronous round trip to a third party, so running it
	// first would hold Gram's own access token valid for the length of someone
	// else's timeout — trading a prompt local revocation for a best-effort
	// remote one. The two are independent controls, so a cache outage must not
	// also cost the upstream revocation.
	s.revoker.RevokeAllDetached(ctx, revokedUpstream)

	if pushErr != nil {
		return oops.E(oops.CodeUnexpected, pushErr, "push jti into revocation cache").LogError(ctx, logger)
	}

	return nil
}
