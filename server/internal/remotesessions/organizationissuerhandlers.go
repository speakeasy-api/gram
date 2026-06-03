package remotesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
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

// CreateOrganizationRemoteSessionIssuer persists a new organization-level
// remote_session_issuer (project_id NULL) in the caller's organization.
func (s *Service) CreateOrganizationRemoteSessionIssuer(ctx context.Context, payload *orgissuersgen.CreateOrganizationRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if strings.TrimSpace(payload.Slug) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").Log(ctx, logger)
	}
	if strings.TrimSpace(payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              payload.Slug,
		Issuer:                            payload.Issuer,
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization remote session issuer").Log(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionIssuerCreate(ctx, dbtx, audit.LogRemoteSessionIssuerCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              uuid.Nil,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(issuer.ID),
		Slug:                   issuer.Slug,
		IssuerURL:              issuer.Issuer,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization remote session issuer creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// UpdateOrganizationRemoteSessionIssuer applies an optional patch to an existing
// organization-level remote_session_issuer.
func (s *Service) UpdateOrganizationRemoteSessionIssuer(ctx context.Context, payload *orgissuersgen.UpdateOrganizationRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, logger)
	}

	// slug and issuer are NOT NULL on the row; reject an explicit empty before
	// the query runs (the SQL treats empty string as "clear to NULL" only for
	// the nullable endpoint columns).
	if payload.Slug != nil && *payload.Slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug cannot be set to empty").Log(ctx, logger)
	}
	if payload.Issuer != nil && *payload.Issuer == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer cannot be set to empty").Log(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "organization remote session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization remote session issuer").Log(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(existing)

	updated, err := txRepo.UpdateOrganizationRemoteSessionIssuer(ctx, repo.UpdateOrganizationRemoteSessionIssuerParams{
		Slug:                              conv.PtrToPGText(payload.Slug),
		Issuer:                            conv.PtrToPGText(payload.Issuer),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		Oidc:                              conv.PtrToPGBool(payload.Oidc),
		Passthrough:                       conv.PtrToPGBool(payload.Passthrough),
		ID:                                issuerID,
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "organization remote session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update organization remote session issuer").Log(ctx, logger)
	}

	afterView := mv.BuildRemoteSessionIssuerView(updated)

	if err := s.auditLogger.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              uuid.Nil,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(updated.ID),
		Slug:                   updated.Slug,
		IssuerURL:              updated.Issuer,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization remote session issuer update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) ListOrganizationRemoteSessionIssuers(ctx context.Context, payload *orgissuersgen.ListOrganizationRemoteSessionIssuersPayload) (*orgissuersgen.ListRemoteSessionIssuersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListOrganizationRemoteSessionIssuers(ctx, repo.ListOrganizationRemoteSessionIssuersParams{
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:         cursor,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization remote session issuers").Log(ctx, s.logger)
	}

	items := make([]*types.RemoteSessionIssuer, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionIssuerView(row))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &orgissuersgen.ListRemoteSessionIssuersResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetOrganizationRemoteSessionIssuer resolves a single organization-level issuer
// by id.
func (s *Service) GetOrganizationRemoteSessionIssuer(ctx context.Context, payload *orgissuersgen.GetOrganizationRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	issuer, err := repo.New(s.db).GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "organization remote session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization remote session issuer").Log(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// DeleteOrganizationRemoteSessionIssuer soft-deletes an organization-level
// issuer. Blocked when any non-deleted remote_session_clients still reference it.
func (s *Service) DeleteOrganizationRemoteSessionIssuer(ctx context.Context, payload *orgissuersgen.DeleteOrganizationRemoteSessionIssuerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	clientCount, err := txRepo.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count remote session clients").Log(ctx, logger)
	}
	if clientCount > 0 {
		return oops.E(oops.CodeConflict, nil, "remote session issuer has active clients; delete the clients first").Log(ctx, logger)
	}

	deleted, err := txRepo.DeleteOrganizationRemoteSessionIssuer(ctx, repo.DeleteOrganizationRemoteSessionIssuerParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete organization remote session issuer").Log(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionIssuerDelete(ctx, dbtx, audit.LogRemoteSessionIssuerDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              uuid.Nil,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(deleted.ID),
		Slug:                   deleted.Slug,
		IssuerURL:              deleted.Issuer,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization remote session issuer deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}
