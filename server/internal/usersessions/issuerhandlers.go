package usersessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Creates an issuer. authn_challenge_mode is "chain" (the issuer
// re-uses an upstream IdP without prompting) or "interactive" (the
// issuer collects user consent before issuing a session).
func (s *Service) CreateUserSessionIssuer(ctx context.Context, payload *gen.CreateUserSessionIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if payload.Slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").Log(ctx, logger)
	}
	if payload.SessionDurationHours <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "session_duration_hours must be positive").Log(ctx, logger)
	}
	dur := time.Duration(payload.SessionDurationHours) * time.Hour

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateUserSessionIssuer(ctx, repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               payload.Slug,
		AuthnChallengeMode: payload.AuthnChallengeMode,
		SessionDuration:    pgtype.Interval{Microseconds: dur.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create user session issuer").Log(ctx, logger)
	}

	if err := audit.LogUserSessionIssuerCreate(ctx, dbtx, audit.LogUserSessionIssuerCreateEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(row.ID),
		Slug:                 row.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return userSessionIssuerView(row), nil
}

// Patches an issuer; nil fields are no-ops.
func (s *Service) UpdateUserSessionIssuer(ctx context.Context, payload *gen.UpdateUserSessionIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	var durPtr *time.Duration
	if payload.SessionDurationHours != nil {
		if *payload.SessionDurationHours <= 0 {
			return nil, oops.E(oops.CodeBadRequest, nil, "session_duration_hours must be positive").Log(ctx, logger)
		}
		parsed := time.Duration(*payload.SessionDurationHours) * time.Hour
		durPtr = &parsed
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").Log(ctx, logger)
	}

	beforeView := userSessionIssuerView(existing)

	updated, err := txRepo.UpdateUserSessionIssuer(ctx, repo.UpdateUserSessionIssuerParams{
		Slug:               conv.PtrToPGText(payload.Slug),
		AuthnChallengeMode: conv.PtrToPGText(payload.AuthnChallengeMode),
		SessionDuration:    conv.PtrToPGInterval(durPtr),
		ID:                 id,
		ProjectID:          *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update user session issuer").Log(ctx, logger)
	}

	afterView := userSessionIssuerView(updated)

	if err := audit.LogUserSessionIssuerUpdate(ctx, dbtx, audit.LogUserSessionIssuerUpdateEvent{
		OrganizationID:                  authCtx.ActiveOrganizationID,
		ProjectID:                       *authCtx.ProjectID,
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                authCtx.Email,
		ActorSlug:                       nil,
		UserSessionIssuerURN:            urn.NewUserSessionIssuer(updated.ID),
		Slug:                            updated.Slug,
		UserSessionIssuerSnapshotBefore: beforeView,
		UserSessionIssuerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

// Lists issuers; keyset paginated by id (descending).
func (s *Service) ListUserSessionIssuers(ctx context.Context, payload *gen.ListUserSessionIssuersPayload) (*gen.ListUserSessionIssuersResult, error) {
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

	rows, err := repo.New(s.db).ListUserSessionIssuersByProjectID(ctx, repo.ListUserSessionIssuersByProjectIDParams{
		ProjectID:  *authCtx.ProjectID,
		Cursor:     cursor,
		LimitValue: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user session issuers").Log(ctx, s.logger)
	}

	items := make([]*types.UserSessionIssuer, len(rows))
	for i, row := range rows {
		items[i] = userSessionIssuerView(row)
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListUserSessionIssuersResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// Fetches an issuer by id or slug. Exactly one must be supplied.
func (s *Service) GetUserSessionIssuer(ctx context.Context, payload *gen.GetUserSessionIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	hasID := payload.ID != nil && *payload.ID != ""
	hasSlug := payload.Slug != nil && *payload.Slug != ""
	if hasID == hasSlug {
		return nil, oops.E(oops.CodeBadRequest, nil, "exactly one of id or slug must be provided").Log(ctx, s.logger)
	}

	var row repo.UserSessionIssuer
	if hasID {
		id, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, s.logger)
		}

		row, err = repo.New(s.db).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
			ID:        id,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").Log(ctx, s.logger)
		}
	} else {
		var err error
		row, err = repo.New(s.db).GetUserSessionIssuerBySlug(ctx, repo.GetUserSessionIssuerBySlugParams{
			Slug:      *payload.Slug,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").Log(ctx, s.logger)
		}
	}

	return userSessionIssuerView(row), nil
}

// Soft-deletes an issuer and cascades to its user_sessions and
// user_session_consents.
func (s *Service) DeleteUserSessionIssuer(ctx context.Context, payload *gen.DeleteUserSessionIssuerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteUserSessionIssuer(ctx, repo.DeleteUserSessionIssuerParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete user session issuer").Log(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteUserSessionsByIssuerID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child user sessions").Log(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteUserSessionConsentsByIssuerID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child user session consents").Log(ctx, logger)
	}

	if err := audit.LogUserSessionIssuerDelete(ctx, dbtx, audit.LogUserSessionIssuerDeleteEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(deleted.ID),
		Slug:                 deleted.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session issuer deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

func userSessionIssuerView(row repo.UserSessionIssuer) *types.UserSessionIssuer {
	dur := time.Duration(row.SessionDuration.Microseconds) * time.Microsecond
	return &types.UserSessionIssuer{
		ID:                   row.ID.String(),
		ProjectID:            row.ProjectID.String(),
		Slug:                 row.Slug,
		AuthnChallengeMode:   row.AuthnChallengeMode,
		SessionDurationHours: int(dur / time.Hour),
		CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// pageLimit clamps the user-supplied limit into the documented range and
// returns it as an int32 ready for sqlc parameters. The clamp guarantees the
// value stays within int32 range.
func pageLimit(in *int) int32 {
	limit := constants.DefaultPageLimit
	if in != nil {
		limit = *in
	}
	if limit <= 0 {
		limit = constants.DefaultPageLimit
	}
	if limit > constants.MaxPageLimit {
		limit = constants.MaxPageLimit
	}
	return int32(limit)
}

// parseCursor decodes a list cursor. Cursors are the id of the last row
// on the previous page; an empty/nil cursor means "start of list".
func parseCursor(cursor *string) (uuid.NullUUID, error) {
	if cursor == nil || *cursor == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*cursor)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("parse cursor: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}
