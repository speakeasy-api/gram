package assets

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	admingen "github.com/speakeasy-api/gram/server/gen/admin_assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/guardian"
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
	defer o11y.LogDefer(ctx, s.logger, "failed to close platform image upload reader", func() error {
		return reader.Close()
	})

	operatorEmail, logger, err := auth.RequireGlobalAdmin(ctx, s.logger)
	if err != nil {
		return nil, fmt.Errorf("authorize global operation: %w", err)
	}

	return s.StorePlatformImage(ctx, logger, operatorEmail, reader, payload.ContentLength, payload.ContentType)
}

// StorePlatformImage stores a publicly served platform-tier image. Callers
// must already have authorized a platform-wide staff write for operatorEmail.
func (s *Service) StorePlatformImage(ctx context.Context, logger *slog.Logger, operatorEmail string, reader io.Reader, contentLength int64, contentType string) (*admingen.UploadImageResult, error) {
	// Bound and validate the original bytes before canonicalization changes their size.
	if contentLength <= 0 || contentLength > MaxFileSizeImage {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid image content length")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxFileSizeImage+1))
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "read image")
	}
	if int64(len(data)) != contentLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "image content length mismatch")
	}
	data, contentType, err = canonicalPlatformImage(data, contentType)
	if err != nil {
		return nil, err
	}
	contentLength = int64(len(data))
	reader = bytes.NewReader(data)
	result, err := s.downloadAuthorizedAsset(ctx, reader, &downloadAuthorizedAssetParams{
		maxLength:     MaxFileSizeImage,
		contentLength: contentLength,
		contentType:   contentType,
	})
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, s.logger, "failed to clean up platform image upload", func() error {
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

	inContentType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse content type: %w", err), "error parsing content type")
	}

	mimeType, ext, err := sniffMimeType(sniffMimeTypeParams{
		contentLength: contentLength,
		inputMimeType: inContentType,
		allowedTypes:  PlatformImageTypes,
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
		contentLength:  contentLength,
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
		ContentLength: contentLength,
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
		attr.SlogAuthUserEmail(operatorEmail),
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

// NewPlatformService supplies only dependencies needed by public image serving and global uploads.
func NewPlatformService(logger *slog.Logger, tp trace.TracerProvider, policy *guardian.Policy, db *pgxpool.Pool, storage BlobStore) *Service {
	logger = logger.With(attr.SlogComponent("assets"))
	return &Service{
		auth:           nil,
		authz:          nil,
		jwtSecret:      "",
		chatSessions:   nil,
		projects:       nil,
		audit:          nil,
		logger:         logger,
		tracer:         tp.Tracer("github.com/speakeasy-api/gram/server/internal/assets"),
		guardianPolicy: policy,
		db:             db,
		storage:        storage,
		repo:           repo.New(db),
	}
}

// canonicalPlatformImage validates XML candidates regardless of their declared MIME.
// Raster declarations retain the existing upload behavior.
func canonicalPlatformImage(data []byte, contentType string) ([]byte, string, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", oops.E(oops.CodeBadRequest, err, "error parsing content type")
	}
	// NUL padding can precede '<' in UTF-16/32 XML, including encoded
	// leading whitespace. Recognize it as XML without decoding or accepting
	// those encodings. The sanitizer still validates the original bytes.
	candidate := strings.TrimLeft(strings.TrimSpace(string(data)), "\x00 \t\r\n")
	// Reject BOMs outright; stripping leading NUL padding also exposes the
	// UTF-32 BOMs as their UTF-16 counterparts. Raster signatures are unaffected.
	for _, bom := range []string{"\xef\xbb\xbf", "\xff\xfe", "\xfe\xff"} {
		if strings.HasPrefix(candidate, bom) {
			return nil, "", oops.E(oops.CodeUnsupportedMedia, nil, "image encoding is unsupported")
		}
	}
	if mediaType == "image/svg+xml" || strings.HasPrefix(candidate, "<") {
		clean, err := sanitizeSVG(data)
		if err != nil {
			return nil, "", oops.E(oops.CodeUnsupportedMedia, err, staticSVGGuidance)
		}
		return clean, "image/svg+xml", nil
	}
	return data, mediaType, nil
}
