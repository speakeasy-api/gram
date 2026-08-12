package assets

import (
	"context"
	"fmt"
	"io"
	"mime"
	"time"

	"github.com/google/uuid"

	admingen "github.com/speakeasy-api/gram/server/gen/admin_assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The adminAssets handlers curate platform-tier asset rows (project_id IS
// NULL AND organization_id IS NULL), shared across every organization. No
// project/org exists to scope an RBAC grant, so each handler gates inline on
// the platform-admin flag; audit is structured-logs only
// (audit_log.organization_id is NOT NULL).

func (s *Service) UploadPlatformImage(ctx context.Context, payload *admingen.UploadPlatformImageForm, reader io.ReadCloser) (*admingen.UploadImageResult, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, err
	}

	result, err := s.downloadPendingAsset(ctx, reader, &downloadPendingAssetParams{
		maxLength:     MaxFileSizeImage,
		contentLength: payload.ContentLength,
		contentType:   payload.ContentType,
	})
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return result.cleanup()
	})

	existing, err := s.findExistingAsset(ctx, &findAssetParams{
		tier:           tierPlatform,
		projectID:      uuid.Nil,
		organizationID: "",
		hash:           result.hash,
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &admingen.UploadImageResult{Asset: &admingen.Asset{
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
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse content type: %w", err), "error parsing content type")
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
		tier:           tierPlatform,
		projectID:      uuid.Nil,
		organizationID: "",
		filename:       filename,
		contentType:    mimeType,
		contentLength:  payload.ContentLength,
		file:           result.file,
	})
	if err != nil {
		return nil, err
	}

	asset, err := s.repo.CreatePlatformAsset(ctx, repo.CreatePlatformAssetParams{
		Name:          filename,
		Url:           uri.String(),
		Sha256:        result.hash,
		Kind:          "image",
		ContentType:   inContentType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create platform asset in database: %w", err), "error saving document info").LogError(ctx, logger)
	}

	// Structured-log audit line standing in for the auditlogs row platform
	// assets can't have (audit_log.organization_id is NOT NULL). Emitted only
	// after the write succeeds so the log never claims a mutation that failed.
	logger.InfoContext(ctx, "platform image asset created",
		attr.SlogAuditAction("create"),
		attr.SlogAuditSubject("asset"),
		attr.SlogAuditSubjectID(urn.NewAsset(urn.AssetKindImage, asset.ID).String()),
		attr.SlogAssetID(asset.ID.String()),
		attr.SlogAuthUserEmail(conv.PtrValOrEmpty(authCtx.Email, "")),
	)

	return &admingen.UploadImageResult{
		Asset: &admingen.Asset{
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
