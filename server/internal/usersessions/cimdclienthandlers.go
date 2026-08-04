// Management API handlers for the userSessionIssuersCimdClients service:
// the read-only CIMD preset catalog Gram curates, and CRUD over the custom
// document URLs an individual issuer additionally admits.
//
// Together with the client_id_metadata_admission_mode field on the
// userSessionIssuers endpoints, these are the whole configuration surface
// for CIMD admission control until the dashboard UI lands (AIS-372).

package usersessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Lists Gram's curated CIMD preset catalog. The catalog is a compile-time
// constant with no tenant data, but the endpoint still carries the standard
// project auth: it is only meaningful next to the issuer configuration it
// accompanies, and an unauthenticated surface would be a needless addition
// to the public attack area.
func (s *Service) ListPresets(ctx context.Context, payload *gen.ListPresetsPayload) (*gen.ListCimdClientPresetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	presets := admission.Catalog()
	items := make([]*types.CimdClientPreset, 0, len(presets))
	for _, preset := range presets {
		items = append(items, &types.CimdClientPreset{
			VendorKey:           preset.VendorKey,
			DisplayName:         preset.DisplayName,
			ClientIDMetadataURI: preset.URL,
			IsPattern:           preset.IsPattern(),
			Enabled:             preset.Enabled,
		})
	}

	return &gen.ListCimdClientPresetsResult{Items: items}, nil
}

// Allows an additional CIMD document URL on an issuer.
func (s *Service) CreateUserSessionIssuerCimdClient(ctx context.Context, payload *gen.CreateUserSessionIssuerCimdClientPayload) (*gen.CreateUserSessionIssuerCimdClientResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserSessionIssuerID(issuerID.String()),
	)

	// Hard-fail on syntax. A URL that cannot be a valid CIMD client_id can
	// never be presented as one, so storing it would only ever produce a
	// confusing dead entry in the issuer's policy. This is the same §3
	// check the authorize path applies, and it includes the length cap.
	if _, err := cimd.ValidateClientIDURL(payload.ClientIDMetadataURI); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_id_metadata_uri: %s", err.Error()).LogError(ctx, logger)
	}

	// Probe BEFORE the write so the warning reflects the URL as saved, but
	// treat every failure as advisory. A vendor's document host being down,
	// slow, or behind a transient error must not block an operator from
	// configuring policy — and the document is re-fetched and re-validated
	// on every authorization anyway, so a bad document cannot be smuggled
	// in by probing at a lucky moment.
	var probeWarning *string
	if _, err := s.cimdResolver.Resolve(ctx, payload.ClientIDMetadataURI); err != nil {
		// The resolver's error text can name internal network conditions
		// (guardian SSRF denials, DNS failures), so it is logged rather
		// than echoed. The operator gets a generic, actionable warning.
		logger.InfoContext(ctx, "cimd custom url probe failed", attr.SlogError(err))
		probeWarning = conv.PtrEmpty("Gram could not fetch and validate a client ID metadata document at this URL. The entry was saved, but a client presenting it will fail to authorize until the document is reachable and valid.")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	row, err := txRepo.CreateUserSessionIssuerCimdClient(ctx, repo.CreateUserSessionIssuerCimdClientParams{
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: issuerID,
		ClientIDMetadataUri: payload.ClientIDMetadataURI,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create user session issuer cimd client").LogError(ctx, logger)
	}

	view := &types.UserSessionIssuerCimdClient{
		ID:                  row.ID.String(),
		ProjectID:           row.ProjectID.String(),
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		ClientIDMetadataURI: row.ClientIDMetadataUri,
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}

	// Only a real new grant is audited. The query is idempotent, so a repeat
	// add returns the existing row; recording an "add" for it would put a
	// second grant of the same URL in the trail at a time when nothing was
	// granted, and an operator reading that history would see a change that
	// never happened.
	if row.Inserted {
		if err := s.audit.LogUserSessionIssuerCimdClientAdd(ctx, dbtx, audit.LogUserSessionIssuerCimdClientAddEvent{
			OrganizationID:        authCtx.ActiveOrganizationID,
			ProjectID:             *authCtx.ProjectID,
			Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:      authCtx.Email,
			ActorSlug:             nil,
			CimdClientURN:         urn.NewUserSessionIssuerCimdClient(row.ID),
			ClientIDMetadataURI:   row.ClientIDMetadataUri,
			CimdClientSnapshot:    view,
			UserSessionIssuerURN:  urn.NewUserSessionIssuer(issuer.ID),
			UserSessionIssuerSlug: issuer.Slug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer cimd client add").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.CreateUserSessionIssuerCimdClientResult{
		Client:       view,
		ProbeWarning: probeWarning,
	}, nil
}

// Lists an issuer's custom CIMD URLs; keyset paginated by id (descending).
func (s *Service) ListUserSessionIssuerCimdClients(ctx context.Context, payload *gen.ListUserSessionIssuerCimdClientsPayload) (*gen.ListUserSessionIssuerCimdClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogError(ctx, s.logger)
	}

	var cursor uuid.NullUUID
	if payload.Cursor != nil {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	limit := pageLimit(payload.Limit)
	rows, err := repo.New(s.db).ListUserSessionIssuerCimdClientsByIssuerID(ctx, repo.ListUserSessionIssuerCimdClientsByIssuerIDParams{
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: issuerID,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user session issuer cimd clients").LogError(ctx, s.logger)
	}

	items := make([]*types.UserSessionIssuerCimdClient, 0, len(rows))
	for _, row := range rows {
		items = append(items, userSessionIssuerCimdClientView(row))
	}

	var nextCursor *string
	if len(rows) == int(limit) {
		nextCursor = conv.PtrEmpty(rows[len(rows)-1].ID.String())
	}

	return &gen.ListUserSessionIssuerCimdClientsResult{Items: items, NextCursor: nextCursor}, nil
}

// Gets a single custom CIMD URL entry by id.
func (s *Service) GetUserSessionIssuerCimdClient(ctx context.Context, payload *gen.GetUserSessionIssuerCimdClientPayload) (*types.UserSessionIssuerCimdClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid id").LogError(ctx, s.logger)
	}

	row, err := repo.New(s.db).GetUserSessionIssuerCimdClientByID(ctx, repo.GetUserSessionIssuerCimdClientByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer cimd client").LogError(ctx, s.logger)
	}

	return userSessionIssuerCimdClientView(row), nil
}

// Removes a custom CIMD URL from an issuer.
func (s *Service) DeleteUserSessionIssuerCimdClient(ctx context.Context, payload *gen.DeleteUserSessionIssuerCimdClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	row, err := txRepo.DeleteUserSessionIssuerCimdClient(ctx, repo.DeleteUserSessionIssuerCimdClientParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return oops.E(oops.CodeUnexpected, err, "delete user session issuer cimd client").LogError(ctx, logger)
	}

	issuer, err := txRepo.GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        row.UserSessionIssuerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionIssuerCimdClientRemove(ctx, dbtx, audit.LogUserSessionIssuerCimdClientRemoveEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		CimdClientURN:         urn.NewUserSessionIssuerCimdClient(row.ID),
		ClientIDMetadataURI:   row.ClientIDMetadataUri,
		CimdClientSnapshot:    userSessionIssuerCimdClientView(row),
		UserSessionIssuerURN:  urn.NewUserSessionIssuer(issuer.ID),
		UserSessionIssuerSlug: issuer.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session issuer cimd client remove").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func userSessionIssuerCimdClientView(row repo.UserSessionIssuerCimdClient) *types.UserSessionIssuerCimdClient {
	return &types.UserSessionIssuerCimdClient{
		ID:                  row.ID.String(),
		ProjectID:           row.ProjectID.String(),
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		ClientIDMetadataURI: row.ClientIDMetadataUri,
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
