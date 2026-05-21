package portals

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/portals/repo"
)

func (s *Service) UpdatePortal(ctx context.Context, payload *gen.UpdatePortalPayload) (*gen.Portal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:      authz.ScopeProjectWrite,
		ResourceID: authCtx.ProjectID.String(),
	}); err != nil {
		return nil, err
	}

	enabled := false
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	var logoID uuid.NullUUID
	if payload.LogoAssetID != nil && *payload.LogoAssetID != "" {
		parsed, err := uuid.Parse(*payload.LogoAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo_asset_id").Log(ctx, s.logger)
		}
		logoID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	r := repo.New(s.db)
	_, err := r.UpsertPortal(ctx, repo.UpsertPortalParams{
		ProjectID:   *authCtx.ProjectID,
		Enabled:     enabled,
		DisplayName: conv.PtrToPGTextEmpty(payload.DisplayName),
		Tagline:     conv.PtrToPGTextEmpty(payload.Tagline),
		LogoAssetID: logoID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert portal").Log(ctx, s.logger)
	}

	// Re-read via GetPortal to get the fully resolved response.
	preview := true
	return s.GetPortal(ctx, &gen.GetPortalPayload{
		ProjectSlugInput: payload.ProjectSlugInput,
		Preview:          &preview,
	})
}
