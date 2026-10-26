package toolsets

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

var (
	// setUserSessionIssuerDeprecatedAt is when toolsets.setUserSessionIssuer started announcing its sunset.
	setUserSessionIssuerDeprecatedAt = time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)

	// setUserSessionIssuerSunset is when toolsets.setUserSessionIssuer is removed (AIM-738).
	setUserSessionIssuerSunset = time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
)

// SetUserSessionIssuer links a session issuer to the toolset's hosted MCP server, or unlinks it when
// user_session_issuer_id is null, through the same write as mcpServers.update.
//
// Deprecated: use mcpServers.update; this endpoint is removed on 2026-11-25.
func (s *Service) SetUserSessionIssuer(ctx context.Context, payload *gen.SetUserSessionIssuerPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetSlug(string(payload.Slug)))
	logDeprecatedHostingUse(ctx, logger, authCtx, nil)

	issuerID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogWarn(ctx, logger)
	}

	toolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{Slug: string(payload.Slug), ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").LogWarn(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load toolset").LogError(ctx, logger)
	}

	if _, err := s.writeHostedServer(ctx, toolset.ID, hostedServerChange{
		Name:                   nil,
		Visibility:             nil,
		UserSessionIssuerID:    issuerID,
		ClearUserSessionIssuer: !issuerID.Valid,
		ToolVariationsGroupID:  nil,
		NetworkAccessMode:      nil,
	}); err != nil {
		return nil, err
	}

	return mv.DescribeToolset(ctx, logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolset.Slug), new(s.toolsetCache.SkipCache()), nil)
}
