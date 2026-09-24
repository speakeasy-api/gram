package usersessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// CreateCimdClient adds an issuer-specific CIMD URL to organization policy.
func (s *Service) CreateCimdClient(ctx context.Context, payload *orggen.CreateCimdClientPayload) (*orggen.CreateUserSessionIssuerCimdClientResult, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogError(ctx, s.logger)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserSessionIssuerID(issuerID.String()),
	)
	if _, err := cimd.ValidateClientIDURL(payload.ClientIDMetadataURI); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_id_metadata_uri: %s", err.Error()).LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)

	if _, err := txRepo.LockOrganizationUserSessionIssuer(ctx, repo.LockOrganizationUserSessionIssuerParams{
		ID:             issuerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization user session issuer").LogError(ctx, logger)
	}
	issuer, err := txRepo.GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer").LogError(ctx, logger)
	}

	row, err := txRepo.CreateOrganizationUserSessionIssuerCimdClient(ctx, repo.CreateOrganizationUserSessionIssuerCimdClientParams{
		ClientIDMetadataUri: payload.ClientIDMetadataURI,
		UserSessionIssuerID: issuerID,
		OrganizationID:      authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create organization user session issuer cimd client").LogError(ctx, logger)
	}

	view := &types.UserSessionIssuerCimdClient{
		ID:                  row.ID.String(),
		ProjectID:           "",
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		ClientIDMetadataURI: row.ClientIDMetadataUri,
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}
	if row.Inserted {
		if err := s.audit.LogUserSessionIssuerCimdClientAdd(ctx, dbtx, audit.LogUserSessionIssuerCimdClientAddEvent{
			OrganizationID:        authCtx.ActiveOrganizationID,
			ProjectID:             uuid.Nil,
			Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:      authCtx.Email,
			ActorSlug:             nil,
			CimdClientURN:         urn.NewUserSessionIssuerCimdClient(row.ID),
			ClientIDMetadataURI:   row.ClientIDMetadataUri,
			CimdClientSnapshot:    view,
			UserSessionIssuerURN:  urn.NewUserSessionIssuer(issuer.ID),
			UserSessionIssuerSlug: issuer.Slug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log organization user session issuer cimd client add").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return &orggen.CreateUserSessionIssuerCimdClientResult{Client: view}, nil
}

// ListCimdClients lists custom CIMD URLs on one organization-owned issuer.
func (s *Service) ListCimdClients(ctx context.Context, payload *orggen.ListCimdClientsPayload) (*orggen.ListUserSessionIssuerCimdClientsResult, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogError(ctx, s.logger)
	}
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}
	limit := pageLimit(payload.Limit)
	rows, err := repo.New(s.db).ListOrganizationUserSessionIssuerCimdClientsByIssuerID(ctx, repo.ListOrganizationUserSessionIssuerCimdClientsByIssuerIDParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: issuerID,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization user session issuer cimd clients").LogError(ctx, s.logger)
	}

	items := make([]*types.UserSessionIssuerCimdClient, 0, len(rows))
	for _, row := range rows {
		items = append(items, userSessionIssuerCimdClientView(row))
	}
	var nextCursor *string
	if len(rows) == int(limit) {
		nextCursor = conv.PtrEmpty(rows[len(rows)-1].ID.String())
	}
	return &orggen.ListUserSessionIssuerCimdClientsResult{Items: items, NextCursor: nextCursor}, nil
}

// GetCimdClient gets one custom CIMD URL owned by the organization.
func (s *Service) GetCimdClient(ctx context.Context, payload *orggen.GetCimdClientPayload) (*types.UserSessionIssuerCimdClient, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid id").LogError(ctx, s.logger)
	}
	row, err := repo.New(s.db).GetOrganizationUserSessionIssuerCimdClientByID(ctx, repo.GetOrganizationUserSessionIssuerCimdClientByIDParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer cimd client").LogError(ctx, s.logger)
	}
	return userSessionIssuerCimdClientView(row), nil
}

// DeleteCimdClient removes one custom CIMD URL from organization policy.
func (s *Service) DeleteCimdClient(ctx context.Context, payload *orggen.DeleteCimdClientPayload) error {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid id").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetOrganizationUserSessionIssuerCimdClientByID(ctx, repo.GetOrganizationUserSessionIssuerCimdClientByIDParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return oops.E(oops.CodeUnexpected, err, "get organization user session issuer cimd client").LogError(ctx, logger)
	}
	if _, err := txRepo.LockOrganizationUserSessionIssuer(ctx, repo.LockOrganizationUserSessionIssuerParams{
		ID:             existing.UserSessionIssuerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock organization user session issuer").LogError(ctx, logger)
	}
	row, err := txRepo.DeleteOrganizationUserSessionIssuerCimdClient(ctx, repo.DeleteOrganizationUserSessionIssuerCimdClientParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return oops.E(oops.CodeUnexpected, err, "delete organization user session issuer cimd client").LogError(ctx, logger)
	}
	issuer, err := txRepo.GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{
		ID:             row.UserSessionIssuerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization user session issuer").LogError(ctx, logger)
	}
	if err := s.audit.LogUserSessionIssuerCimdClientRemove(ctx, dbtx, audit.LogUserSessionIssuerCimdClientRemoveEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             uuid.Nil,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		CimdClientURN:         urn.NewUserSessionIssuerCimdClient(row.ID),
		ClientIDMetadataURI:   row.ClientIDMetadataUri,
		CimdClientSnapshot:    userSessionIssuerCimdClientView(row),
		UserSessionIssuerURN:  urn.NewUserSessionIssuer(issuer.ID),
		UserSessionIssuerSlug: issuer.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization user session issuer cimd client remove").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return nil
}
