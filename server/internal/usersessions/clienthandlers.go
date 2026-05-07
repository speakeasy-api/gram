package usersessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
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

// Lists DCR-registered clients; keyset paginated by id (descending).
// client_secret_hash is stripped from the view.
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}

	issuerFilter, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").Log(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListUserSessionClientsByProjectID(ctx, repo.ListUserSessionClientsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: issuerFilter,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user session clients").Log(ctx, s.logger)
	}

	items := make([]*types.UserSessionClient, len(rows))
	for i, row := range rows {
		items[i] = mv.BuildUserSessionClientView(row)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client id").Log(ctx, s.logger)
	}

	row, err := repo.New(s.db).GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session client").Log(ctx, s.logger)
	}

	return mv.BuildUserSessionClientView(repo.UserSessionClient{
		ID:                    row.ID,
		ProjectID:             row.ProjectID,
		UserSessionIssuerID:   row.UserSessionIssuerID,
		ClientID:              row.ClientID,
		ClientSecretHash:      row.ClientSecretHash,
		ClientName:            row.ClientName,
		RedirectUris:          row.RedirectUris,
		ClientIDIssuedAt:      row.ClientIDIssuedAt,
		ClientSecretExpiresAt: row.ClientSecretExpiresAt,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		DeletedAt:             row.DeletedAt,
		Deleted:               row.Deleted,
	}), nil
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
		return oops.E(oops.CodeBadRequest, err, "invalid client id").Log(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeUserSessionClient(ctx, repo.RevokeUserSessionClientParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session client not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke user session client").Log(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteUserSessionsByClientID(ctx, uuid.NullUUID{UUID: revoked.ID, Valid: true}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child user sessions").Log(ctx, logger)
	}

	if err := audit.LogUserSessionClientRevoke(ctx, dbtx, audit.LogUserSessionClientRevokeEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionClientURN: urn.NewUserSessionClient(revoked.ID),
		ClientID:             revoked.ClientID,
		ClientName:           revoked.ClientName,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session client revocation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}
