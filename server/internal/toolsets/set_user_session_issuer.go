package toolsets

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsR "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// SetUserSessionIssuer links a toolset to a user_session_issuer (or unlinks
// when user_session_issuer_id is null). The USI must already exist in the
// caller's project. The link lives on toolsets.user_session_issuer_id with
// ON DELETE SET NULL, so deleting the USI later silently unlinks the
// toolset.
func (s *Service) SetUserSessionIssuer(ctx context.Context, payload *gen.SetUserSessionIssuerPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	beforeView, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), new(s.toolsetCache.SkipCache()))
	if err != nil {
		return nil, err
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: beforeView.ID, Dimensions: nil}); err != nil {
		return nil, err
	}

	var usiID uuid.NullUUID
	if payload.UserSessionIssuerID != nil {
		parsed, parseErr := uuid.Parse(*payload.UserSessionIssuerID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid user_session_issuer_id").Log(ctx, s.logger)
		}
		usiID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Validate that the target USI lives in the caller's project before
	// writing the FK so a request can't graft an unrelated tenant's USI
	// onto this toolset via cross-project id.
	if usiID.Valid {
		if _, err := usersessionsR.New(dbtx).GetUserSessionIssuerByID(ctx, usersessionsR.GetUserSessionIssuerByIDParams{
			ID:        usiID.UUID,
			ProjectID: *authCtx.ProjectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "load user session issuer").Log(ctx, s.logger)
		}
	}

	if _, err := s.repo.WithTx(dbtx).UpdateToolsetUserSessionIssuer(ctx, repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: usiID,
		Slug:                string(payload.Slug),
		ProjectID:           *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update toolset user_session_issuer").Log(ctx, s.logger)
	}

	afterView, err := mv.DescribeToolset(ctx, s.logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), new(s.toolsetCache.SkipCache()))
	if err != nil {
		return nil, err
	}

	toolsetUUID, err := uuid.Parse(afterView.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "invalid toolset id").Log(ctx, s.logger)
	}

	if err := s.audit.LogToolsetUpdate(ctx, dbtx, audit.LogToolsetUpdateEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		ToolsetURN:            urn.NewToolset(toolsetUUID),
		ToolsetName:           afterView.Name,
		ToolsetSlug:           string(afterView.Slug),
		ToolsetVersionAfter:   afterView.ToolsetVersion,
		ToolsetSnapshotBefore: beforeView,
		ToolsetSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log toolset update").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, s.logger)
	}

	return afterView, nil
}
