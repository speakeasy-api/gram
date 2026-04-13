package skills

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/http/skills/server"
	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	maxCaptureArtifactMiB   = 10
	maxCaptureArtifactBytes = maxCaptureArtifactMiB * 1024 * 1024
	skillAssetKind          = "skill"
)

var allowedCaptureContentTypes = map[string]struct{}{
	"application/zip":              {},
	"application/x-zip-compressed": {},
	"application/x-zip":            {},
}

type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	auth    *auth.Auth
	db      *pgxpool.Pool
	storage assets.BlobStore
	repo    *assetsrepo.Queries
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionsMgr *sessions.Manager,
	storage assets.BlobStore,
	accessLoader auth.AccessLoader,
) *Service {
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skills"),
		logger:  logger.With(attr.SlogComponent("skills")),
		auth:    auth.New(logger, db, sessionsMgr, accessLoader),
		db:      db,
		storage: storage,
		repo:    assetsrepo.New(db),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	server.Mount(
		mux,
		server.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) Capture(ctx context.Context, payload *gen.CaptureSkillForm, reader io.ReadCloser) (*gen.CaptureSkillResult, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if payload.ContentLength <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length must be > 0")
	}
	if payload.ContentLength > maxCaptureArtifactBytes {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length exceeds %d MiB limit", maxCaptureArtifactMiB)
	}

	mediaType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse content type: %w", err), "invalid content type")
	}
	if _, exists := allowedCaptureContentTypes[mediaType]; !exists {
		return nil, oops.E(oops.CodeUnsupportedMedia, nil, "unsupported content type: %s", mediaType)
	}

	file, err := os.CreateTemp("", "skill-capture-*")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create temp file: %w", err), "error buffering skill artifact")
	}
	defer o11y.NoLogDefer(func() error {
		_ = file.Close()
		return os.Remove(file.Name())
	})

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, h), io.LimitReader(reader, payload.ContentLength+1))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy artifact bytes: %w", err), "error buffering skill artifact")
	}
	if n != payload.ContentLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length mismatch")
	}

	contentSHA := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(contentSHA, payload.ContentSha256) {
		return nil, oops.E(oops.CodeBadRequest, nil, "content sha256 mismatch")
	}

	existing, err := s.findExistingAsset(ctx, *authCtx.ProjectID, contentSHA)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &gen.CaptureSkillResult{Asset: existing}, nil
	}

	filename := fmt.Sprintf("skill-%s.zip", contentSHA)
	uri, err := s.uploadAsset(ctx, *authCtx.ProjectID, filename, mediaType, payload.ContentLength, file)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing skill assets").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	asset, err := s.repo.WithTx(dbtx).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     *authCtx.ProjectID,
		Sha256:        contentSHA,
		Kind:          skillAssetKind,
		ContentType:   mediaType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			existing, findErr := s.findExistingAsset(ctx, *authCtx.ProjectID, contentSHA)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return &gen.CaptureSkillResult{Asset: existing}, nil
			}
			return nil, oops.E(oops.CodeConflict, nil, "skill asset already exists with incompatible metadata")
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create skill asset: %w", err), "error saving skill artifact")
	}
	if asset.Kind != skillAssetKind {
		return nil, oops.E(oops.CodeConflict, nil, "skill asset hash conflicts with existing non-skill asset")
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save skill artifact").Log(ctx, s.logger)
	}

	return &gen.CaptureSkillResult{
		Asset: &gen.CaptureSkillAsset{
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

func (s *Service) archiveStaleSkillAsset(ctx context.Context, projectID uuid.UUID, assetID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE assets
		SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE project_id = $1
		  AND id = $2
		  AND deleted IS FALSE
	`, projectID, assetID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, fmt.Errorf("archive stale skill asset: %w", err), "error loading skill asset")
	}

	return nil
}

func (s *Service) findExistingAsset(ctx context.Context, projectID uuid.UUID, sha string) (*gen.CaptureSkillAsset, error) {
	asset, err := s.repo.GetProjectAssetBySHA256(ctx, assetsrepo.GetProjectAssetBySHA256Params{
		ProjectID: projectID,
		Sha256:    sha,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("find existing skill asset: %w", err), "error loading skill asset")
	}
	if asset.Deleted {
		return nil, nil
	}
	if asset.Kind != skillAssetKind {
		return nil, oops.E(oops.CodeConflict, nil, "skill asset hash conflicts with existing non-skill asset")
	}

	assetURL, err := url.Parse(asset.Url)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid existing asset url", attr.SlogURLFull(asset.Url), attr.SlogError(err))
		return nil, nil
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check existing skill asset: %w", err), "error loading skill asset")
	}
	if !exists {
		if err := s.archiveStaleSkillAsset(ctx, projectID, asset.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}

	return &gen.CaptureSkillAsset{
		ID:            asset.ID.String(),
		Kind:          asset.Kind,
		Sha256:        asset.Sha256,
		ContentType:   asset.ContentType,
		ContentLength: asset.ContentLength,
		CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) uploadAsset(ctx context.Context, projectID uuid.UUID, filename string, contentType string, contentLength int64, file *os.File) (*url.URL, error) {
	dst, uri, err := s.storage.Write(ctx, path.Join(projectID.String(), filename), contentType, contentLength)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("write to blob storage: %w", err), "error uploading skill artifact")
	}
	dstClosed := false
	defer o11y.LogDefer(ctx, s.logger, func() error {
		if dstClosed {
			return nil
		}
		if err := dst.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("close blob storage writer: %w", err)
		}
		return nil
	})

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("seek skill file: %w", err), "error uploading skill artifact")
	}

	n, err := io.Copy(dst, file)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy to blob storage: %w", err), "error uploading skill artifact")
	}
	if n != contentLength {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("expected %d bytes, wrote %d", contentLength, n), "error uploading skill artifact")
	}

	if err := dst.Close(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("finalize blob storage: %w", err), "error uploading skill artifact")
	}
	dstClosed = true

	return uri, nil
}
