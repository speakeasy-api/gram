package usersessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
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

// Lists consent records; keyset paginated by id (descending).
func (s *Service) ListUserSessionConsents(ctx context.Context, payload *gen.ListUserSessionConsentsPayload) (*gen.ListUserSessionConsentsResult, error) {
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

	clientFilter, err := conv.PtrToNullUUID(payload.UserSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_client_id").Log(ctx, s.logger)
	}
	issuerFilter, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").Log(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListUserSessionConsentsByProjectID(ctx, repo.ListUserSessionConsentsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		SubjectUrn:          conv.PtrToPGTextEmpty(payload.SubjectUrn),
		UserSessionClientID: clientFilter,
		UserSessionIssuerID: issuerFilter,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user session consents").Log(ctx, s.logger)
	}

	items := make([]*types.UserSessionConsent, len(rows))
	for i, row := range rows {
		items[i] = mv.BuildUserSessionConsentView(repo.UserSessionConsent{
			ID:                  row.ID,
			ProjectID:           row.ProjectID,
			SubjectUrn:          row.SubjectUrn,
			UserSessionClientID: row.UserSessionClientID,
			RemoteSetHash:       row.RemoteSetHash,
			ConsentedAt:         row.ConsentedAt,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
			DeletedAt:           row.DeletedAt,
			Deleted:             row.Deleted,
		})
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListUserSessionConsentsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// Withdraws consent. Subsequent authorization requests for matching
// (subject, client) pairs re-prompt.
func (s *Service) RevokeUserSessionConsent(ctx context.Context, payload *gen.RevokeUserSessionConsentPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid consent id").Log(ctx, s.logger)
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

	revoked, err := txRepo.RevokeUserSessionConsent(ctx, repo.RevokeUserSessionConsentParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session consent not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke user session consent").Log(ctx, logger)
	}

	if err := audit.LogUserSessionConsentRevoke(ctx, dbtx, audit.LogUserSessionConsentRevokeEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		UserSessionConsentURN: urn.NewUserSessionConsent(revoked.ID),
		Principal:             revoked.SubjectUrn,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session consent revocation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}
