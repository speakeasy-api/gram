package usersessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func (s *Service) requireOrganizationIssuerScope(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.APIKeyID != "" {
		return nil, oops.E(oops.CodeForbidden, nil, "organization user session issuer management requires a user session")
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	return authCtx, nil
}

// lockTrustedRemoteSessionIssuer validates the trust target before and after
// taking the advisory lock shared with remote-issuer delete, move, and migrate
// operations. The first read avoids locking arbitrary cross-organization IDs;
// the second closes the race where a valid row changes scope or is deleted
// while this transaction waits for the lock.
func lockTrustedRemoteSessionIssuer(ctx context.Context, q *remotesessionsrepo.Queries, id uuid.UUID, organizationID string) error {
	params := remotesessionsrepo.GetTrustedRemoteSessionIssuerForOrganizationParams{
		ID:             id,
		OrganizationID: organizationID,
	}
	if _, err := q.GetTrustedRemoteSessionIssuerForOrganization(ctx, params); err != nil {
		return fmt.Errorf("get trusted remote session issuer before lock: %w", err)
	}
	if err := q.LockRemoteSessionIssuerForClientBinding(ctx, id); err != nil {
		return fmt.Errorf("lock trusted remote session issuer: %w", err)
	}
	if _, err := q.GetTrustedRemoteSessionIssuerForOrganization(ctx, params); err != nil {
		return fmt.Errorf("get trusted remote session issuer after lock: %w", err)
	}
	return nil
}

func parseTrustedRemoteSessionIssuerID(raw *string, allowClear bool) (uuid.NullUUID, error) {
	if raw == nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		if allowClear {
			return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
		}
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("trusted_remote_session_issuer_id cannot be empty")
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("invalid trusted_remote_session_issuer_id: %w", err)
	}
	return conv.ToNullUUID(id), nil
}

// CreateIssuer creates an organization-owned issuer inherited by every project
// in the caller's organization.
func (s *Service) CreateIssuer(ctx context.Context, payload *orggen.CreateIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	if payload.Slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").LogError(ctx, logger)
	}
	dur, err := sessionDurationFromHours(payload.SessionDurationHours)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid session_duration_hours: %v", err).LogError(ctx, logger)
	}
	trustedIssuerID, err := parseTrustedRemoteSessionIssuerID(payload.TrustedRemoteSessionIssuerID, false)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if trustedIssuerID.Valid {
		if err := lockTrustedRemoteSessionIssuer(ctx, remotesessionsrepo.New(dbtx), trustedIssuerID.UUID, authCtx.ActiveOrganizationID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "trusted remote session issuer not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate trusted remote session issuer").LogError(ctx, logger)
		}
	}

	row, err := repo.New(dbtx).CreateOrganizationUserSessionIssuer(ctx, repo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                         payload.Slug,
		AuthnChallengeMode:           payload.AuthnChallengeMode,
		SessionDuration:              pgtype.Interval{Microseconds: dur.Microseconds(), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: trustedIssuerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization user session issuer").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionIssuerCreate(ctx, dbtx, audit.LogUserSessionIssuerCreateEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            uuid.Nil,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(row.ID),
		Slug:                 row.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization user session issuer creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return UserSessionIssuerView(row), nil
}

// ListIssuers lists only organization-owned issuers. Project-owned issuers are
// managed through the project service.
func (s *Service) ListIssuers(ctx context.Context, payload *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}
	rows, err := repo.New(s.db).ListOrganizationUserSessionIssuers(ctx, repo.ListOrganizationUserSessionIssuersParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Cursor:         cursor,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization user session issuers").LogError(ctx, s.logger)
	}

	items := make([]*types.UserSessionIssuer, len(rows))
	for i, row := range rows {
		items[i] = UserSessionIssuerView(row)
	}
	var nextCursor *string
	if len(rows) >= int(limit) {
		value := rows[len(rows)-1].ID.String()
		nextCursor = &value
	}

	return &orggen.ListOrganizationUserSessionIssuersResult{Items: items, NextCursor: nextCursor}, nil
}

// GetIssuer resolves an organization-owned issuer by id.
func (s *Service) GetIssuer(ctx context.Context, payload *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, s.logger)
	}
	row, err := repo.New(s.db).GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer").LogError(ctx, s.logger)
	}
	return UserSessionIssuerView(row), nil
}

// UpdateIssuer patches an organization-owned issuer.
func (s *Service) UpdateIssuer(ctx context.Context, payload *orggen.UpdateIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}
	if payload.Slug != nil && *payload.Slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug cannot be empty").LogError(ctx, logger)
	}
	var durPtr *time.Duration
	if payload.SessionDurationHours != nil {
		parsed, parseErr := sessionDurationFromHours(*payload.SessionDurationHours)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid session_duration_hours: %v", parseErr).LogError(ctx, logger)
		}
		durPtr = &parsed
	}
	if payload.ClientIDMetadataAdmissionMode != nil && !admission.IsValidMode(*payload.ClientIDMetadataAdmissionMode) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id_metadata_admission_mode must be one of %v", admission.Modes()).LogError(ctx, logger)
	}
	trustedIssuerID, err := parseTrustedRemoteSessionIssuerID(payload.TrustedRemoteSessionIssuerID, true)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer").LogError(ctx, logger)
	}
	beforeView := UserSessionIssuerView(existing)

	if payload.TrustedRemoteSessionIssuerID != nil && trustedIssuerID.Valid {
		if err := lockTrustedRemoteSessionIssuer(ctx, remotesessionsrepo.New(dbtx), trustedIssuerID.UUID, authCtx.ActiveOrganizationID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "trusted remote session issuer not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate trusted remote session issuer").LogError(ctx, logger)
		}
	}
	trustedIssuerIDParam := conv.PtrToPGText(payload.TrustedRemoteSessionIssuerID)
	if payload.TrustedRemoteSessionIssuerID != nil && trustedIssuerID.Valid {
		trustedIssuerIDParam = conv.ToPGText(trustedIssuerID.UUID.String())
	}

	updated, err := txRepo.UpdateOrganizationUserSessionIssuer(ctx, repo.UpdateOrganizationUserSessionIssuerParams{
		Slug:                          conv.PtrToPGText(payload.Slug),
		AuthnChallengeMode:            conv.PtrToPGText(payload.AuthnChallengeMode),
		SessionDuration:               conv.PtrToPGInterval(durPtr),
		ClientIDMetadataAdmissionMode: conv.PtrToPGText(payload.ClientIDMetadataAdmissionMode),
		TrustedRemoteSessionIssuerID:  trustedIssuerIDParam,
		ID:                            id,
		OrganizationID:                authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update organization user session issuer").LogError(ctx, logger)
	}
	afterView := UserSessionIssuerView(updated)

	if err := s.audit.LogUserSessionIssuerUpdate(ctx, dbtx, audit.LogUserSessionIssuerUpdateEvent{
		OrganizationID:                  authCtx.ActiveOrganizationID,
		ProjectID:                       uuid.Nil,
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                authCtx.Email,
		ActorSlug:                       nil,
		UserSessionIssuerURN:            urn.NewUserSessionIssuer(updated.ID),
		Slug:                            updated.Slug,
		UserSessionIssuerSnapshotBefore: beforeView,
		UserSessionIssuerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization user session issuer update").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return afterView, nil
}

// GetIssuerDeletePreflight returns the same live owner set DeleteIssuer uses
// as its blocking condition, plus informational client and session counts.
func (s *Service) GetIssuerDeletePreflight(ctx context.Context, payload *orggen.GetIssuerDeletePreflightPayload) (*orggen.OrganizationUserSessionIssuerDeletePreflight, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, s.logger)
	}

	q := repo.New(s.db)
	if _, err := q.GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer").LogError(ctx, s.logger)
	}

	return organizationIssuerDeletePreflight(ctx, s.logger, q, id, authCtx.ActiveOrganizationID)
}

// DeleteIssuer soft-deletes an organization-owned issuer once no live MCP
// server or toolset references it.
func (s *Service) DeleteIssuer(ctx context.Context, payload *orggen.DeleteIssuerPayload) error {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)
	if err := txRepo.LockUserSessionIssuerForOwnerBinding(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock user session issuer for owner binding").LogError(ctx, logger)
	}
	if _, err := txRepo.LockOrganizationUserSessionIssuer(ctx, repo.LockOrganizationUserSessionIssuerParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock organization user session issuer").LogError(ctx, logger)
	}

	preflight, err := organizationIssuerDeletePreflight(ctx, logger, txRepo, id, authCtx.ActiveOrganizationID)
	if err != nil {
		return err
	}
	if !preflight.CanDelete {
		return oops.E(oops.CodeConflict, nil, "user session issuer is still in use by an active MCP server or toolset")
	}

	deleted, err := txRepo.DeleteOrganizationUserSessionIssuer(ctx, repo.DeleteOrganizationUserSessionIssuerParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeConflict, err, "user session issuer became referenced during deletion").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete organization user session issuer").LogError(ctx, logger)
	}

	orphanCreds, err := s.revoker.DetachOrganizationUserSessionIssuerFromClients(ctx, dbtx, deleted.ID, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach remote session clients from organization user session issuer").LogError(ctx, logger)
	}
	if _, err := txRepo.SoftDeleteUserSessionsByIssuerID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child user sessions").LogError(ctx, logger)
	}
	if _, err := txRepo.SoftDeleteUserSessionConsentsByIssuerID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child user session consents").LogError(ctx, logger)
	}
	if err := s.audit.LogUserSessionIssuerDelete(ctx, dbtx, audit.LogUserSessionIssuerDeleteEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            uuid.Nil,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(deleted.ID),
		Slug:                 deleted.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization user session issuer deletion").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	s.revoker.RevokeAllDetached(ctx, orphanCreds)
	return nil
}

type organizationIssuerPreflightQueries interface {
	CountOrganizationUserSessionIssuerClients(context.Context, repo.CountOrganizationUserSessionIssuerClientsParams) (int32, error)
	CountOrganizationUserSessionIssuerLiveSessions(context.Context, repo.CountOrganizationUserSessionIssuerLiveSessionsParams) (int32, error)
	ListOrganizationUserSessionIssuerMCPServers(context.Context, repo.ListOrganizationUserSessionIssuerMCPServersParams) ([]repo.ListOrganizationUserSessionIssuerMCPServersRow, error)
	ListOrganizationUserSessionIssuerToolsets(context.Context, repo.ListOrganizationUserSessionIssuerToolsetsParams) ([]repo.ListOrganizationUserSessionIssuerToolsetsRow, error)
}

func organizationIssuerDeletePreflight(ctx context.Context, logger *slog.Logger, q organizationIssuerPreflightQueries, id uuid.UUID, organizationID string) (*orggen.OrganizationUserSessionIssuerDeletePreflight, error) {
	clients, err := q.CountOrganizationUserSessionIssuerClients(ctx, repo.CountOrganizationUserSessionIssuerClientsParams{UserSessionIssuerID: id, OrganizationID: organizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count organization user session issuer clients").LogError(ctx, logger)
	}
	sessions, err := q.CountOrganizationUserSessionIssuerLiveSessions(ctx, repo.CountOrganizationUserSessionIssuerLiveSessionsParams{UserSessionIssuerID: id, OrganizationID: organizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count organization user session issuer live sessions").LogError(ctx, logger)
	}
	mcpRows, err := q.ListOrganizationUserSessionIssuerMCPServers(ctx, repo.ListOrganizationUserSessionIssuerMCPServersParams{UserSessionIssuerID: id, OrganizationID: organizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization user session issuer MCP servers").LogError(ctx, logger)
	}
	toolsetRows, err := q.ListOrganizationUserSessionIssuerToolsets(ctx, repo.ListOrganizationUserSessionIssuerToolsetsParams{UserSessionIssuerID: id, OrganizationID: organizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization user session issuer toolsets").LogError(ctx, logger)
	}

	mcpServers := make([]*orggen.OrganizationUserSessionIssuerReference, 0, len(mcpRows))
	for _, row := range mcpRows {
		mcpServers = append(mcpServers, &orggen.OrganizationUserSessionIssuerReference{ID: row.ID.String(), Name: row.Name, ProjectID: row.ProjectID.String(), ProjectName: row.ProjectName})
	}
	toolsets := make([]*orggen.OrganizationUserSessionIssuerReference, 0, len(toolsetRows))
	for _, row := range toolsetRows {
		toolsets = append(toolsets, &orggen.OrganizationUserSessionIssuerReference{ID: row.ID.String(), Name: row.Name, ProjectID: row.ProjectID.String(), ProjectName: row.ProjectName})
	}
	return &orggen.OrganizationUserSessionIssuerDeletePreflight{
		ClientCount:      int(clients),
		LiveSessionCount: int(sessions),
		McpServers:       mcpServers,
		Toolsets:         toolsets,
		CanDelete:        len(mcpServers) == 0 && len(toolsets) == 0,
	}, nil
}
