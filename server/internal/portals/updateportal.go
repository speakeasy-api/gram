package portals

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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

	// Load the existing row (if any) so we can merge partial updates: nil
	// payload fields preserve the existing value, empty strings explicitly
	// clear, non-empty strings set the new value.
	r := repo.New(s.db)
	existing, err := r.GetPortalByProjectID(ctx, *authCtx.ProjectID)
	rowExists := true
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		rowExists = false
		existing = repo.ProjectPortal{}
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load portal").Log(ctx, s.logger)
	}

	enabled := existing.Enabled
	if !rowExists {
		enabled = false
	}
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	displayName := mergePGText(payload.DisplayName, existing.DisplayName)
	tagline := mergePGText(payload.Tagline, existing.Tagline)

	logoID, err := mergeLogoAssetID(payload.LogoAssetID, existing.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo_asset_id").Log(ctx, s.logger)
	}

	_, err = r.UpsertPortal(ctx, repo.UpsertPortalParams{
		ProjectID:   *authCtx.ProjectID,
		Enabled:     enabled,
		DisplayName: displayName,
		Tagline:     tagline,
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

// mergePGText applies partial-update semantics for nullable text columns:
//   - payload == nil          → preserve existing
//   - payload == &""          → explicitly clear (NULL)
//   - payload == &"non-empty" → set new value
func mergePGText(payload *string, existing pgtype.Text) pgtype.Text {
	if payload == nil {
		return existing
	}
	if *payload == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *payload, Valid: true}
}

// mergeLogoAssetID applies partial-update semantics for the nullable
// logo_asset_id UUID column. An invalid UUID string (other than the empty
// string used to clear) returns an error to the caller.
func mergeLogoAssetID(payload *string, existing uuid.NullUUID) (uuid.NullUUID, error) {
	if payload == nil {
		return existing, nil
	}
	if *payload == "" {
		return uuid.NullUUID{}, nil
	}
	parsed, err := uuid.Parse(*payload)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}
