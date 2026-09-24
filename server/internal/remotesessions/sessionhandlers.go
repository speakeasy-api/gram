package remotesessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
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

func (s *Service) ListRemoteSessions(ctx context.Context, payload *gen.ListRemoteSessionsPayload) (*gen.ListRemoteSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	var op *bindingOperation
	eligibleOnly := payload.PrincipalID != nil || payload.UserSessionIssuerID != nil
	if eligibleOnly {
		if payload.PrincipalID == nil || payload.UserSessionIssuerID == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "principal_id and user_session_issuer_id must be supplied together")
		}
		var err error
		op, err = s.beginBindingOperation(ctx, *payload.PrincipalID, *payload.UserSessionIssuerID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = op.tx.Rollback(ctx) }()
	} else if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	subjectFilter := conv.PtrToPGText(payload.SubjectUrn)
	clientFilter, err := conv.PtrToNullUUID(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client_id").LogError(ctx, logger)
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	var rows []repo.ListRemoteSessionsByProjectIDRow
	if eligibleOnly {
		candidates, err := op.queries.ListPrincipalRemoteSessionCandidates(ctx, repo.ListPrincipalRemoteSessionCandidatesParams{
			ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject,
			Cursor: cursor, ClientFilter: clientFilter, LimitValue: pgtype.Int4{Int32: limit, Valid: true},
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list eligible remote sessions").LogError(ctx, logger)
		}
		for _, row := range candidates {
			if subjectFilter.Valid && row.RemoteSession.SubjectUrn.String() != subjectFilter.String {
				continue
			}
			rows = append(rows, repo.ListRemoteSessionsByProjectIDRow(row))
		}
	} else {
		rows, err = repo.New(s.db).ListRemoteSessionsByProjectID(ctx, repo.ListRemoteSessionsByProjectIDParams{
			ProjectID:             *authCtx.ProjectID,
			OrganizationID:        authCtx.ActiveOrganizationID,
			SubjectUrn:            subjectFilter,
			RemoteSessionClientID: clientFilter,
			Cursor:                cursor,
			LimitValue:            limit,
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote sessions").LogError(ctx, logger)
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

	return &gen.ListRemoteSessionsResult{
		Items:      items,
		NextCursor: nextCursor,
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
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{
		ID:             sessionID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "revoke remote session").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionDelete(ctx, dbtx, audit.LogRemoteSessionDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RemoteSessionURN: urn.NewRemoteSession(revoked.ID),
		SubjectURN:       revoked.SubjectUrn,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session revoke").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Tell the upstream to drop the credentials the row was holding. Strictly
	// after the commit and strictly best-effort: the local revoke the caller
	// asked for has already taken effect, and an issuer that is slow, down, or
	// simply advertises no revocation endpoint must not turn a successful
	// revoke into a failed request. See upstreamrevoke.go.
	s.revoker.RevokeDetached(ctx, RevokedCredentials{
		RemoteSessionClientID: revoked.RemoteSessionClientID,
		AccessTokenEncrypted:  revoked.AccessTokenEncrypted,
		RefreshTokenEncrypted: revoked.RefreshTokenEncrypted,
	})

	return nil
}
