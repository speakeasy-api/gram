package assets

import (
	"context"
	"fmt"
	"io"
	"mime"
	"time"

	"github.com/google/uuid"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The organizationAssets handlers manage organization-tier asset rows
// (project_id IS NULL, organization_id set), inherited by every project in
// the organization. Writes are gated on org:admin against the caller's active
// organization.

func (s *Service) UploadOrganizationImage(ctx context.Context, payload *orggen.UploadOrganizationImageForm, reader io.ReadCloser) (*orggen.UploadImageResult, error) {
	defer o11y.LogDefer(ctx, s.logger, "failed to close organization image upload reader", func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	result, err := s.downloadPendingAsset(ctx, reader, &downloadPendingAssetParams{
		maxLength:     MaxFileSizeImage,
		contentLength: payload.ContentLength,
		contentType:   payload.ContentType,
	})
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, s.logger, "failed to clean up organization image upload", func() error {
		return result.cleanup()
	})

	existing, err := s.findExistingAsset(ctx, &findAssetParams{
		tier:           tierOrganization,
		projectID:      uuid.Nil,
		organizationID: authCtx.ActiveOrganizationID,
		hash:           result.hash,
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &orggen.UploadImageResult{Asset: &orggen.Asset{
			ID:            existing.ID,
			Kind:          existing.Kind,
			Sha256:        existing.Sha256,
			ContentType:   existing.ContentType,
			ContentLength: existing.ContentLength,
			CreatedAt:     existing.CreatedAt,
			UpdatedAt:     existing.UpdatedAt,
		}}, nil
	}

	inContentType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse content type: %w", err), "error parsing content type")
	}

	mimeType, ext, err := sniffMimeType(sniffMimeTypeParams{
		contentLength: payload.ContentLength,
		inputMimeType: inContentType,
		allowedTypes:  []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
	})
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("image-%s%s", result.hash, ext)
	uri, err := s.uploadAsset(ctx, &uploadAssetParams{
		tier:           tierOrganization,
		projectID:      uuid.Nil,
		organizationID: authCtx.ActiveOrganizationID,
		filename:       filename,
		contentType:    mimeType,
		contentLength:  payload.ContentLength,
		file:           result.file,
	})
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing image assets").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	ar := s.repo.WithTx(dbtx)

	asset, err := ar.CreateOrganizationAsset(ctx, repo.CreateOrganizationAssetParams{
		Name:           filename,
		Url:            uri.String(),
		OrganizationID: authCtx.ActiveOrganizationID,
		Sha256:         result.hash,
		Kind:           "image",
		ContentType:    inContentType,
		ContentLength:  payload.ContentLength,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create organization asset in database: %w", err), "error saving document info").LogError(ctx, logger)
	}

	if err := s.audit.LogAssetCreate(ctx, dbtx, audit.LogAssetCreateEvent{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,

		AssetURN:  urn.NewAsset(urn.AssetKindImage, asset.ID),
		AssetName: asset.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save image asset creation audit log").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save image asset").LogError(ctx, logger)
	}

	return &orggen.UploadImageResult{
		Asset: &orggen.Asset{
			ID:            asset.ID.String(),
			Kind:          asset.Kind,
			Sha256:        asset.Sha256,
			ContentType:   asset.ContentType,
			ContentLength: asset.ContentLength,
			CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
		},
	}, nil
}
