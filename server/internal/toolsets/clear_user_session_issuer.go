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
)

// ClearUserSessionIssuer unlinks any user_session_issuer attached to a
// toolset by nulling toolsets.user_session_issuer_id. The underlying USI row
// is left untouched. Calling this on a toolset that already has no USI
// returns the toolset unchanged.
func (s *Service) ClearUserSessionIssuer(ctx context.Context, payload *gen.ClearUserSessionIssuerPayload) (*types.Toolset, error) {
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := s.repo.WithTx(dbtx).UpdateToolsetUserSessionIssuer(ctx, repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:                string(payload.Slug),
		ProjectID:           *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "clear toolset user_session_issuer").Log(ctx, s.logger)
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
