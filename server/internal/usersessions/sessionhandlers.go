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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}

	issuerFilter, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").Log(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		SubjectUrn:          conv.PtrToPGTextEmpty(payload.SubjectUrn),
		UserSessionIssuerID: issuerFilter,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user sessions").Log(ctx, s.logger)
	}

	items := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		items[i] = mv.BuildUserSessionView(row)
	}

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

// Soft-deletes the session and pushes its jti into the revocation cache
// so the access token stops validating before its TTL expires.
func (s *Service) RevokeUserSession(ctx context.Context, payload *gen.RevokeUserSessionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid session id").Log(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeUserSession(ctx, repo.RevokeUserSessionParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke user session").Log(ctx, logger)
	}

	if err := audit.LogUserSessionRevoke(ctx, dbtx, audit.LogUserSessionRevokeEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		UserSessionURN:   urn.NewUserSession(revoked.ID),
		Principal:        revoked.SubjectUrn,
		Jti:              revoked.Jti,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session revocation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	// Push the jti into the revocation cache after the DB commit so a cached
	// jti always corresponds to a soft-deleted row. Cache-write failure is
	// surfaced as Unexpected — the row is gone but the access token would keep
	// validating until expiry, which is the case the cache exists to prevent.
	if err := s.chatSessions.RevokeToken(ctx, revoked.Jti); err != nil {
		return oops.E(oops.CodeUnexpected, err, "push jti into revocation cache").Log(ctx, logger)
	}

	return nil
}
