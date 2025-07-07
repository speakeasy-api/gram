package assets

import (
	"bufio"
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
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/assets"
	srv "github.com/speakeasy-api/gram/server/gen/http/assets/server"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	auth    *auth.Auth
	storage BlobStore

	projects *projectsRepo.Queries
	repo     *repo.Queries
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, storage BlobStore) *Service {
	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/server/internal/assets"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions),
		storage:  storage,
		projects: projectsRepo.New(db),
		repo:     repo.New(db),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) ServeImage(ctx context.Context, payload *gen.ServeImageForm) (*gen.ServeImageResult, io.ReadCloser, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	assetID, err := uuid.Parse(payload.ID)
	if err != nil || assetID == uuid.Nil {
		return nil, nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse asset id: %w", err), "invalid asset id")
	}

	row, err := s.repo.GetImageAssetURL(ctx, assetID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get image asset url: %w", err), "error loading asset")
	}

	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse asset url: %w", err), "error loading asset")
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check if asset exists: %w", err), "error loading asset")
	}

	if !exists {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	body, err := s.storage.Read(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("read asset: %w", err), "error fetching asset")
	}

	return &gen.ServeImageResult{
		ContentType:   row.ContentType,
		ContentLength: row.ContentLength,
		LastModified:  row.UpdatedAt.Time.Format(time.RFC1123),
	}, body, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) UploadImage(ctx context.Context, payload *gen.UploadImageForm, reader io.ReadCloser) (res *gen.UploadImageResult, err error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	result, err := s.downloadPendingAsset(ctx, reader, &downloadPendingAssetParams{
		maxLength:     4 * 1024 * 1024,
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
		projectID: *authCtx.ProjectID,
		hash:      result.hash,
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &gen.UploadImageResult{Asset: existing}, nil
	}

	inContentType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse content type: %w", err), "error parsing content type")
	}

	mimeType, ext, err := sniffMimeType(result.file, sniffMimeTypeParams{
		contentLength: payload.ContentLength,
		inputMimeType: inContentType,
		allowedTypes:  []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
	})
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("image-%s%s", result.hash, ext)
	uri, err := s.uploadAsset(ctx, &uploadAssetParams{
		projectID:     *authCtx.ProjectID,
		filename:      filename,
		contentType:   mimeType,
		contentLength: payload.ContentLength,
		file:          result.file,
	})
	if err != nil {
		return nil, err
	}

	asset, err := s.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     *authCtx.ProjectID,
		Sha256:        result.hash,
		Kind:          "image",
		ContentType:   inContentType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create asset in database: %w", err), "error saving document info")
	}

	return &gen.UploadImageResult{
		Asset: &gen.Asset{
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

func (s *Service) UploadOpenAPIv3(ctx context.Context, payload *gen.UploadOpenAPIv3Form, reader io.ReadCloser) (*gen.UploadOpenAPIv3Result, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	result, err := s.downloadPendingAsset(ctx, reader, &downloadPendingAssetParams{
		maxLength:     10 * 1024 * 1024,
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
		projectID: *authCtx.ProjectID,
		hash:      result.hash,
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &gen.UploadOpenAPIv3Result{Asset: existing}, nil
	}

	contentType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse content type: %w", err), "error parsing content type")
	}

	mimeType, ext, err := sniffMimeType(result.file, sniffMimeTypeParams{
		contentLength: payload.ContentLength,
		inputMimeType: contentType,
		allowedTypes:  []string{"application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml", "application/json", "text/json"},
	})
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("openapi-%s%s", result.hash, ext)
	uri, err := s.uploadAsset(ctx, &uploadAssetParams{
		projectID:     *authCtx.ProjectID,
		filename:      filename,
		contentType:   mimeType,
		contentLength: payload.ContentLength,
		file:          result.file,
	})
	if err != nil {
		return nil, err
	}

	asset, err := s.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     *authCtx.ProjectID,
		Sha256:        result.hash,
		Kind:          "openapiv3",
		ContentType:   contentType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create asset in database: %w", err), "error saving document info")
	}

	return &gen.UploadOpenAPIv3Result{
		Asset: &gen.Asset{
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

type downloadPendingAssetParams struct {
	maxLength     int64
	contentLength int64
	contentType   string
}
type downloadPendingAssetResult struct {
	file    *os.File
	hash    string
	cleanup func() error
}

func (s *Service) downloadPendingAsset(ctx context.Context, reader io.Reader, params *downloadPendingAssetParams) (*downloadPendingAssetResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if params.contentLength == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "no content")
	}

	if params.contentLength > params.maxLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length exceeds 8 MiB limit")
	}

	f, err := os.CreateTemp("", "asset-*")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create temp file: %w", err), "error downloading document")
	}

	cleanup := func() error {
		return os.Remove(f.Name())
	}
	defer func() {
		if err == nil {
			// caller will clean up file after using it
			return
		}

		if cerr := cleanup(); cerr != nil {
			s.logger.ErrorContext(ctx, "failed to cleanup temp file", slog.String("error", cerr.Error()))
		}
	}()

	h := sha256.New()
	writer := bufio.NewWriterSize(io.MultiWriter(f, h), 4096)
	_, err = io.Copy(writer, io.LimitReader(reader, params.contentLength))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy to temp file: %w", err), "error downloading file")
	}
	if err := writer.Flush(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("flush writer: %w", err), "error downloading file")
	}

	off, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("seek to start: %w", err), "error reading file")
	}
	if off != 0 {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("seek to start: offset not 0: %d", off), "error reading file")
	}

	return &downloadPendingAssetResult{
		file:    f,
		hash:    hex.EncodeToString(h.Sum(nil)),
		cleanup: cleanup,
	}, nil
}

type findAssetParams struct {
	projectID uuid.UUID
	hash      string
}

func (s *Service) findExistingAsset(ctx context.Context, params *findAssetParams) (*gen.Asset, error) {
	asset, err := s.repo.GetProjectAssetBySHA256(ctx, repo.GetProjectAssetBySHA256Params{
		ProjectID: params.projectID,
		Sha256:    params.hash,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("find project asset by hash: %w", err), "error loading document data")
	}
	if asset.ID != uuid.Nil {
		if assetURL, err := url.Parse(asset.Url); err == nil {
			exists, err := s.storage.Exists(ctx, assetURL)
			switch {
			case err != nil:
				s.logger.ErrorContext(ctx, "failed to check if asset exists", slog.String("url", asset.Url), slog.String("error", err.Error()))
			case exists:
				return &gen.Asset{
					ID:            asset.ID.String(),
					Kind:          asset.Kind,
					Sha256:        asset.Sha256,
					ContentType:   asset.ContentType,
					ContentLength: asset.ContentLength,
					CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
					UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
				}, nil
			default:
				// it doesn't exist, carry on to create it
			}
		} else {
			s.logger.ErrorContext(ctx, "failed to parse asset url", slog.String("url", asset.Url), slog.String("error", err.Error()))
		}
	}

	return nil, nil
}

type uploadAssetParams struct {
	projectID     uuid.UUID
	filename      string
	contentType   string
	contentLength int64
	file          *os.File
}

func (s *Service) uploadAsset(ctx context.Context, params *uploadAssetParams) (*url.URL, error) {
	projectID := params.projectID
	dst, uri, err := s.storage.Write(ctx, path.Join(projectID.String(), params.filename), params.file, params.contentType)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("write to blob storage: %w", err), "error writing document")
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		if err := dst.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("close blob storage: %w", err)
		}
		return nil
	})

	n, err := io.CopyBuffer(dst, params.file, make([]byte, 4096))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy to blob storage: %w", err), "error uploading document")
	}
	if n != params.contentLength {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("expected %d bytes, wrote %d", params.contentLength, n), "error uploading document")
	}

	if err := dst.Close(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("finalize blob storage: %w", err), "error uploading document")
	}

	return uri, nil
}

type sniffMimeTypeParams struct {
	contentLength int64
	inputMimeType string
	allowedTypes  []string
}

func sniffMimeType(source io.ReadSeeker, params sniffMimeTypeParams) (mtype string, ext string, err error) {
	if err := inv.Check(
		"sniff mime type parameters",
		"contentLength is set", params.contentLength != 0,
		"inputMimeType is set", params.inputMimeType != "",
		"allowedTypes is set", len(params.allowedTypes) > 0,
	); err != nil {
		return "", "", oops.E(oops.CodeUnexpected, err, "error checking content type")
	}

	if !slices.Contains(params.allowedTypes, params.inputMimeType) {
		return "", "", oops.E(oops.CodeUnsupportedMedia, nil, "unsupported content type: %s (allowed: %s)", params.inputMimeType, strings.Join(params.allowedTypes, ", "))
	}

	var exts []string
	switch params.inputMimeType {
	case "image/jpeg":
		exts = []string{".jpg"}
	case "image/png":
		exts = []string{".png"}
	case "image/gif":
		exts = []string{".gif"}
	case "image/webp":
		exts = []string{".webp"}
	case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
		exts = []string{".yaml"}
	case "application/json", "text/json":
		exts = []string{".json"}
	default:
		exts, err = mime.ExtensionsByType(params.inputMimeType)
		if err != nil {
			return "", "", oops.E(oops.CodeUnsupportedMedia, err, "unsupported content type: %s", params.inputMimeType)
		}
	}

	if len(exts) == 0 {
		exts = []string{""}
	}

	return params.inputMimeType, exts[0], nil
}
