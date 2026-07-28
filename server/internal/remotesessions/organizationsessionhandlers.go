package remotesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgsessionsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// clientUpstreamResource derives the RFC 8707 resource for a client's
// sessions from its attached MCP servers. Exactly one distinct upstream URL
// binds the audience; zero or multiple return "" so the parameter is omitted
// (matching pre-resource behavior — an ambiguous multi-upstream client can't
// be bound to a single audience). Url is empty for non-remote backends.
func clientUpstreamResource(rows []repo.ListOrganizationMcpServersForClientRow) string {
	resource := ""
	for _, row := range rows {
		url := strings.TrimRight(row.Url, "/")
		if url == "" {
			continue
		}
		if resource != "" && resource != url {
			return ""
		}
		resource = url
	}
	return resource
}

// ListClientSessions lists the sessions minted against a client in the
// caller's organization.
func (s *Service) ListClientSessions(ctx context.Context, payload *orgsessionsgen.ListClientSessionsPayload) (*orgsessionsgen.ListOrganizationRemoteSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListOrganizationRemoteSessionsByClientID(ctx, repo.ListOrganizationRemoteSessionsByClientIDParams{
		RemoteSessionClientID: clientID,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization admin remote sessions").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSession, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionView(row.RemoteSession, conv.FromPGText[string](row.SubjectDisplayName), conv.FromPGText[string](row.SubjectEmail)))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSession.ID.String()
		nextCursor = &c
	}

	return &orgsessionsgen.ListOrganizationRemoteSessionsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// RevokeSession soft-deletes a single session in the caller's organization.
func (s *Service) RevokeSession(ctx context.Context, payload *orgsessionsgen.RevokeSessionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	sessionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeOrganizationRemoteSession(ctx, repo.RevokeOrganizationRemoteSessionParams{
		ID:             sessionID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "revoke organization admin remote session").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionDelete(ctx, dbtx, audit.LogRemoteSessionDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        orgProjectID(revoked.ClientProjectID),
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RemoteSessionURN: urn.NewRemoteSession(revoked.ID),
		SubjectURN:       revoked.SubjectUrn,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization admin remote session revoke").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// RefreshSession forces an upstream token refresh on a single session in the
// caller's organization, regardless of current access-token expiry, and returns
// the updated session view.
//
// The upstream token POST is an external call, so the refresh (load client →
// POST → upsert) runs on the pool, never inside a transaction; only the audit
// log is wrapped in a short transaction afterwards.
func (s *Service) RefreshSession(ctx context.Context, payload *orgsessionsgen.RefreshSessionPayload) (*types.RemoteSession, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	sessionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	row, err := repo.New(s.db).GetOrganizationRemoteSessionByID(ctx, repo.GetOrganizationRemoteSessionByIDParams{
		ID:             sessionID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session").LogError(ctx, logger)
	}

	// Defense in depth: the UI hides the action for sessions without a refresh
	// token, but the gate is not authoritative.
	if !row.RemoteSession.RefreshTokenEncrypted.Valid || row.RemoteSession.RefreshTokenEncrypted.String == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "remote session has no refresh token").LogError(ctx, logger)
	}

	// Recover the session's RFC 8707 audience binding from the client's
	// attached MCP servers so the refreshed token keeps it.
	mcpRows, err := repo.New(s.db).ListOrganizationMcpServersForClient(ctx, row.RemoteSession.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for client").LogError(ctx, logger)
	}

	// Refresh on the pool — the upstream token POST must not run inside a tx.
	updated, _, err := refreshSessionTokens(ctx, repo.New(s.db), s.enc, s.policy, row.RemoteSession, clientUpstreamResource(mcpRows))
	if err != nil {
		// Operator-actionable failures carry a public-safe reason; surface it so
		// the admin sees why the refresh failed instead of a generic error.
		var refreshErr *TokenRefreshError
		if errors.As(err, &refreshErr) {
			return nil, oops.E(oops.CodeBadRequest, err, "Unable to refresh: %s", refreshErr.Reason).LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "refresh organization admin remote session").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.auditLogger.LogRemoteSessionRefresh(ctx, dbtx, audit.LogRemoteSessionRefreshEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        orgProjectID(row.ClientProjectID),
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RemoteSessionURN: urn.NewRemoteSession(updated.ID),
		SubjectURN:       updated.SubjectUrn,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session refresh").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionView(updated, conv.FromPGText[string](row.SubjectDisplayName), conv.FromPGText[string](row.SubjectEmail)), nil
}

// RevokeAllClientSessions soft-deletes all sessions minted against a client
// in the caller's organization, recording a single bulk audit event.
func (s *Service) RevokeAllClientSessions(ctx context.Context, payload *orgsessionsgen.RevokeAllClientSessionsPayload) (*orgsessionsgen.RevokeAllRemoteSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	clientRow, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}
	client := clientRow.RemoteSessionClient

	revokedCount, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, client.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke all remote sessions").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientRevokeSessions(ctx, dbtx, audit.LogRemoteSessionClientRevokeSessionsEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(client.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		RevokedCount:           revokedCount,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session revoke-all").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &orgsessionsgen.RevokeAllRemoteSessionsResult{RevokedCount: int(revokedCount)}, nil
}
