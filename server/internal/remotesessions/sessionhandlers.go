package remotesessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) ListRemoteSessions(ctx context.Context, payload *gen.ListRemoteSessionsPayload) (*gen.ListRemoteSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	principalFilter := conv.PtrToPGText(payload.PrincipalUrn)
	clientFilter, err := conv.PtrToNullUUID(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client_id").Log(ctx, logger)
	}

	rows, err := repo.New(s.db).ListRemoteSessionsByProjectID(ctx, repo.ListRemoteSessionsByProjectIDParams{
		ProjectID:             *authCtx.ProjectID,
		PrincipalUrn:          principalFilter,
		RemoteSessionClientID: clientFilter,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote sessions").Log(ctx, logger)
	}

	items := make([]*types.RemoteSession, 0, len(rows))
	for _, row := range rows {
		items = append(items, remoteSessionView(row))
	}

	return &gen.ListRemoteSessionsResult{
		Items:      items,
		NextCursor: nil,
	}, nil
}

func (s *Service) RevokeRemoteSession(ctx context.Context, payload *gen.RevokeRemoteSessionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	sessionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{
		ID:        sessionID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "remote session not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke remote session").Log(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionDelete(ctx, dbtx, audit.LogRemoteSessionDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RemoteSessionURN: urn.NewRemoteSession(revoked.ID),
		PrincipalURN:     revoked.PrincipalUrn,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session revoke").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

func remoteSessionView(row repo.RemoteSession) *types.RemoteSession {
	var refreshExpiresAt *string
	if row.RefreshExpiresAt.Valid {
		v := row.RefreshExpiresAt.Time.Format(time.RFC3339)
		refreshExpiresAt = &v
	}
	return &types.RemoteSession{
		ID:                    row.ID.String(),
		PrincipalUrn:          row.PrincipalUrn.String(),
		UserSessionIssuerID:   row.UserSessionIssuerID.String(),
		RemoteSessionClientID: row.RemoteSessionClientID.String(),
		AccessExpiresAt:       row.AccessExpiresAt.Time.Format(time.RFC3339),
		RefreshExpiresAt:      refreshExpiresAt,
		Scopes:                row.Scopes,
		CreatedAt:             row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
