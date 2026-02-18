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
	"net/http"
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
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const (
	mib                       = 1024 * 1024
	MaxFileSizeFunctions      = 15 * mib
	MaxFileSizeOpenAPI        = 10 * mib
	MaxFileSizeImage          = 4 * mib
	MaxFileSizeChatAttachment = 10 * mib
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	auth      *auth.Auth
	storage   BlobStore
	jwtSecret string

	chatSessions *chatsessions.Manager
	projects     *projectsRepo.Queries
	repo         *repo.Queries
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, chatSessions *chatsessions.Manager, storage BlobStore, jwtSecret string) *Service {
	logger = logger.With(attr.SlogComponent("assets"))

	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/server/internal/assets"),
		logger:       logger,
		db:           db,
		auth:         auth.New(logger, db, sessions),
		storage:      storage,
		jwtSecret:    jwtSecret,
		chatSessions: chatSessions,
		projects:     projectsRepo.New(db),
		repo:         repo.New(db),
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) JWTAuth(ctx context.Context, token string, schema *security.JWTScheme) (context.Context, error) {
	return s.chatSessions.Authorize(ctx, token)
}

func (s *Service) ListAssets(ctx context.Context, payload *gen.ListAssetsPayload) (*gen.ListAssetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	assets, err := s.repo.ListAssets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("list assets: %w", err), "error listing assets")
	}

	assetsResult := make([]*gen.Asset, len(assets))
	for i, asset := range assets {
		assetsResult[i] = &gen.Asset{
			ID:            asset.ID.String(),
			Kind:          asset.Kind,
			Sha256:        asset.Sha256,
			ContentType:   asset.ContentType,
			ContentLength: asset.ContentLength,
			CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.ListAssetsResult{
		Assets: assetsResult,
	}, nil
}

func (s *Service) ServeImage(ctx context.Context, payload *gen.ServeImageForm) (*gen.ServeImageResult, io.ReadCloser, error) {
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
		ContentType:              row.ContentType,
		ContentLength:            row.ContentLength,
		LastModified:             row.UpdatedAt.Time.Format(time.RFC1123),
		AccessControlAllowOrigin: conv.Ptr("*"),
	}, body, nil
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

func (s *Service) UploadFunctions(ctx context.Context, payload *gen.UploadFunctionsForm, reader io.ReadCloser) (*gen.UploadFunctionsResult, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	result, err := s.downloadPendingAsset(ctx, reader, &downloadPendingAssetParams{
		maxLength:     MaxFileSizeFunctions,
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
		return &gen.UploadFunctionsResult{Asset: existing}, nil
	}

	contentType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse content type: %w", err), "error parsing content type")
	}

	mimeType, ext, err := sniffMimeType(sniffMimeTypeParams{
		contentLength: payload.ContentLength,
		inputMimeType: contentType,
		allowedTypes:  []string{"application/zip", "application/x-zip-compressed", "application/x-zip"},
	})
	if err != nil {
		return nil, err
	}

	if err := validateFunctionsArchive(ctx, s.logger, result.file.Name()); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid functions archive: %s", err.Error()).Log(ctx, s.logger)
	}

	filename := fmt.Sprintf("functions-%s%s", result.hash, ext)
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
		Kind:          "functions",
		ContentType:   contentType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create asset in database: %w", err), "error saving document info")
	}

	return &gen.UploadFunctionsResult{
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
		maxLength:     MaxFileSizeOpenAPI,
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

	mimeType, ext, err := sniffMimeType(sniffMimeTypeParams{
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
			s.logger.ErrorContext(ctx, "failed to cleanup temp file", attr.SlogError(cerr))
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
				s.logger.ErrorContext(ctx, "failed to check if asset exists", attr.SlogURLFull(asset.Url), attr.SlogError(err))
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
			s.logger.ErrorContext(ctx, "failed to parse asset url", attr.SlogURLFull(asset.Url), attr.SlogError(err))
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
	dst, uri, err := s.storage.Write(ctx, path.Join(projectID.String(), params.filename), params.contentType, params.contentLength)
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

func sniffMimeType(params sniffMimeTypeParams) (mtype string, ext string, err error) {
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
	case "application/zip", "application/x-zip-compressed", "application/x-zip":
		exts = []string{".zip"}
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

func (s *Service) ServeOpenAPIv3(ctx context.Context, payload *gen.ServeOpenAPIv3Form) (*gen.ServeOpenAPIv3Result, io.ReadCloser, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	assetID, err := uuid.Parse(payload.ID)
	switch {
	case err != nil:
		return nil, nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse asset id: %w", err), "invalid asset id")
	case assetID == uuid.Nil:
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "asset id cannot be empty")
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
	}
	if projectID == uuid.Nil {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "project id cannot be empty")
	}

	// This check is important to ensure the client has access to the project they specified in the request.
	if err := s.auth.CheckProjectAccess(ctx, s.logger, projectID); err != nil {
		return nil, nil, err
	}

	logger := s.logger.With(
		attr.SlogAssetID(assetID.String()),
		attr.SlogProjectID(projectID.String()),
	)

	row, err := s.repo.GetOpenAPIv3AssetURL(ctx, repo.GetOpenAPIv3AssetURLParams{
		ID:        assetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get openapiv3 asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check if asset exists: %w", err), "error loading asset").Log(ctx, logger)
	}

	if !exists {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	body, err := s.storage.Read(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("read asset: %w", err), "error fetching asset").Log(ctx, logger)
	}

	return &gen.ServeOpenAPIv3Result{
		ContentType:   row.ContentType,
		ContentLength: row.ContentLength,
		LastModified:  row.UpdatedAt.Time.Format(time.RFC1123),
	}, body, nil
}

func (s *Service) FetchOpenAPIv3FromURL(ctx context.Context, payload *gen.FetchOpenAPIv3FromURLForm) (*gen.UploadOpenAPIv3Result, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Validate URL
	parsedURL, err := url.Parse(payload.URL)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse url: %w", err), "invalid URL")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, oops.E(oops.CodeBadRequest, nil, "URL must use http or https scheme")
	}

	// Fetch the OpenAPI spec from the URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, payload.URL, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create request: %w", err), "error fetching URL")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("fetch url: %w", err), "error fetching URL")
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return nil, oops.E(oops.CodeBadRequest, nil, "failed to fetch URL: received status %d", resp.StatusCode)
	}

	// Determine content type from response or URL
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || strings.HasPrefix(contentType, "text/plain") {
		// Infer from URL extension
		ext := strings.ToLower(path.Ext(parsedURL.Path))
		switch ext {
		case ".yaml", ".yml":
			contentType = "application/yaml"
		case ".json":
			contentType = "application/json"
		default:
			contentType = "application/yaml"
		}
	}

	// Validate content type is an allowed OpenAPI format
	allowedContentTypes := []string{
		"application/yaml",
		"application/x-yaml",
		"text/yaml",
		"text/x-yaml",
		"application/json",
		"text/json",
	}
	// Check if any allowed content type appears in the contentType string
	// (handles malformed headers with multiple types like "application/octet-stream, application/json")
	hasAllowedType := slices.ContainsFunc(allowedContentTypes, func(allowed string) bool {
		return strings.Contains(contentType, allowed)
	})
	if !hasAllowedType {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported content type: %s. Expected YAML or JSON", contentType)
	}

	// Parse media type to get just the mime type without parameters for logging/storage
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// If parsing fails, try to extract the first valid content type we found
		for _, allowed := range allowedContentTypes {
			if strings.Contains(contentType, allowed) {
				mediaType = allowed
				break
			}
		}
		if mediaType == "" {
			mediaType = contentType
		}
	}

	contentLength := resp.ContentLength
	if contentLength <= 0 {
		contentLength = MaxFileSizeOpenAPI
	}
	if contentLength > MaxFileSizeOpenAPI {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length exceeds 10 MiB limit")
	}

	result, err := s.downloadPendingAsset(ctx, resp.Body, &downloadPendingAssetParams{
		maxLength:     MaxFileSizeOpenAPI,
		contentLength: contentLength,
		contentType:   mediaType,
	})
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return result.cleanup()
	})

	// Get actual file size
	fileInfo, err := result.file.Stat()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("stat temp file: %w", err), "error reading file")
	}
	actualContentLength := fileInfo.Size()

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

	mimeType, ext, err := sniffMimeType(sniffMimeTypeParams{
		contentLength: actualContentLength,
		inputMimeType: mediaType,
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
		contentLength: actualContentLength,
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
		ContentType:   mediaType,
		ContentLength: actualContentLength,
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

func (s *Service) ServeFunction(ctx context.Context, payload *gen.ServeFunctionForm) (*gen.ServeFunctionResult, io.ReadCloser, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	assetID, err := uuid.Parse(payload.ID)
	switch {
	case err != nil:
		return nil, nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse asset id: %w", err), "invalid asset id")
	case assetID == uuid.Nil:
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "asset id cannot be empty")
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
	}
	if projectID == uuid.Nil {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "project id cannot be empty")
	}

	// This check is important to ensure the client has access to the project they specified in the request.
	if err := s.auth.CheckProjectAccess(ctx, s.logger, projectID); err != nil {
		return nil, nil, err
	}

	logger := s.logger.With(
		attr.SlogAssetID(assetID.String()),
		attr.SlogProjectID(projectID.String()),
	)

	row, err := s.repo.GetFunctionAssetURL(ctx, repo.GetFunctionAssetURLParams{
		ID:        assetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get function asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check if asset exists: %w", err), "error loading asset").Log(ctx, logger)
	}

	if !exists {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	body, err := s.storage.Read(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("read asset: %w", err), "error fetching asset").Log(ctx, logger)
	}

	return &gen.ServeFunctionResult{
		ContentType:   row.ContentType,
		ContentLength: row.ContentLength,
		LastModified:  row.UpdatedAt.Time.Format(time.RFC1123),
	}, body, nil
}

func validateChatAttachmentContentType(contentType string) (mimeType, ext string, err error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", "", oops.E(oops.CodeBadRequest, err, "invalid content type")
	}

	// Check wildcard categories
	allowedPrefixes := []string{"audio/", "image/", "text/"}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(mediaType, prefix) {
			exts, _ := mime.ExtensionsByType(mediaType)
			if len(exts) > 0 {
				return mediaType, exts[0], nil
			}
			return mediaType, "", nil
		}
	}

	// Check explicit application types
	explicitTypeExtensions := map[string]string{
		"application/pdf":    ".pdf",
		"application/json":   ".json",
		"application/yaml":   ".yaml",
		"application/x-yaml": ".yaml",
	}
	extension, ok := explicitTypeExtensions[mediaType]
	if ok {
		return mediaType, extension, nil
	}

	return "", "", oops.E(oops.CodeUnsupportedMedia, nil,
		"unsupported content type: %s (allowed: audio/*, image/*, text/*, application/pdf, application/json, application/yaml)",
		mediaType)
}

func (s *Service) UploadChatAttachment(ctx context.Context, payload *gen.UploadChatAttachmentForm, reader io.ReadCloser) (*gen.UploadChatAttachmentResult, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	result, err := s.downloadPendingAsset(ctx, reader, &downloadPendingAssetParams{
		maxLength:     MaxFileSizeChatAttachment,
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
		existingServeURL := url.URL{
			Path: srv.ServeChatAttachmentAssetsPath(),
			RawQuery: url.Values{
				"project_id": {authCtx.ProjectID.String()},
				"id":         {existing.ID},
			}.Encode(),
		}
		return &gen.UploadChatAttachmentResult{Asset: existing, URL: existingServeURL.String()}, nil
	}

	mimeType, ext, err := validateChatAttachmentContentType(payload.ContentType)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("attachment-%s%s", result.hash, ext)
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
		Kind:          "chat_attachment",
		ContentType:   mimeType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create asset in database: %w", err), "error saving document info")
	}

	serveURL := url.URL{
		Path: srv.ServeChatAttachmentAssetsPath(),
		RawQuery: url.Values{
			"project_id": {authCtx.ProjectID.String()},
			"id":         {asset.ID.String()},
		}.Encode(),
	}

	return &gen.UploadChatAttachmentResult{
		Asset: &gen.Asset{
			ID:            asset.ID.String(),
			Kind:          asset.Kind,
			Sha256:        asset.Sha256,
			ContentType:   asset.ContentType,
			ContentLength: asset.ContentLength,
			CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
		},
		URL: serveURL.String(),
	}, nil
}

func (s *Service) ServeChatAttachment(ctx context.Context, payload *gen.ServeChatAttachmentForm) (*gen.ServeChatAttachmentResult, io.ReadCloser, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	assetID, err := uuid.Parse(payload.ID)
	switch {
	case err != nil:
		return nil, nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse asset id: %w", err), "invalid asset id")
	case assetID == uuid.Nil:
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "asset id cannot be empty")
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
	}
	if projectID == uuid.Nil {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "project id cannot be empty")
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, projectID); err != nil {
		return nil, nil, err
	}

	logger := s.logger.With(
		attr.SlogAssetID(assetID.String()),
		attr.SlogProjectID(projectID.String()),
	)

	row, err := s.repo.GetChatAttachmentAssetURL(ctx, repo.GetChatAttachmentAssetURLParams{
		ID:        assetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get chat attachment asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check if asset exists: %w", err), "error loading asset").Log(ctx, logger)
	}

	if !exists {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	body, err := s.storage.Read(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("read asset: %w", err), "error fetching asset").Log(ctx, logger)
	}

	return &gen.ServeChatAttachmentResult{
		ContentType:   row.ContentType,
		ContentLength: row.ContentLength,
		LastModified:  row.UpdatedAt.Time.Format(time.RFC1123),
	}, body, nil
}

const (
	defaultSignedURLTTL time.Duration = 10 * time.Minute
	maxSignedURLTTL     time.Duration = 1 * time.Hour
)

func (s *Service) CreateSignedChatAttachmentURL(ctx context.Context, payload *gen.CreateSignedChatAttachmentURLForm) (*gen.CreateSignedChatAttachmentURLResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	assetID, err := uuid.Parse(payload.ID)
	switch {
	case err != nil:
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse asset id: %w", err), "invalid asset id")
	case assetID == uuid.Nil:
		return nil, oops.E(oops.CodeBadRequest, nil, "asset id cannot be empty")
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
	}
	if projectID == uuid.Nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "project id cannot be empty")
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, projectID); err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogAssetID(assetID.String()),
		attr.SlogProjectID(projectID.String()),
	)

	// Verify asset exists and belongs to project
	_, err = s.repo.GetChatAttachmentAssetURL(ctx, repo.GetChatAttachmentAssetURLParams{
		ID:        assetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get chat attachment asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	// Determine TTL
	ttl := defaultSignedURLTTL
	if payload.TTLSeconds != nil && *payload.TTLSeconds > 0 {
		sec := time.Duration(*payload.TTLSeconds) * time.Second
		ttl = min(sec, maxSignedURLTTL)
	}

	// Generate signed token
	token, expiresAt, err := GenerateSignedAssetToken(s.jwtSecret, assetID, projectID, ttl)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("generate signed token: %w", err), "error creating signed url").Log(ctx, logger)
	}

	// Build URL
	signedURL := url.URL{
		Path:     srv.ServeChatAttachmentSignedAssetsPath(),
		RawQuery: url.Values{"token": {token}}.Encode(),
	}

	return &gen.CreateSignedChatAttachmentURLResult{
		URL:       signedURL.String(),
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}

func (s *Service) ServeChatAttachmentSigned(ctx context.Context, payload *gen.ServeChatAttachmentSignedForm) (*gen.ServeChatAttachmentSignedResult, io.ReadCloser, error) {
	// Validate and parse the signed token
	claims, err := ValidateSignedAssetToken(s.jwtSecret, payload.Token)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnauthorized, err, "invalid or expired token")
	}

	assetID, err := uuid.Parse(claims.AssetID)
	switch {
	case err != nil:
		return nil, nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse asset id from token: %w", err), "invalid token")
	case assetID == uuid.Nil:
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "invalid token")
	}

	projectID, err := uuid.Parse(claims.ProjectID)
	switch {
	case err != nil:
		return nil, nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse project id from token: %w", err), "invalid token")
	case projectID == uuid.Nil:
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "invalid token")
	}

	logger := s.logger.With(
		attr.SlogAssetID(assetID.String()),
		attr.SlogProjectID(projectID.String()),
	)

	// Fetch asset metadata
	row, err := s.repo.GetChatAttachmentAssetURL(ctx, repo.GetChatAttachmentAssetURLParams{
		ID:        assetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get chat attachment asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse asset url: %w", err), "error loading asset").Log(ctx, logger)
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check if asset exists: %w", err), "error loading asset").Log(ctx, logger)
	}

	if !exists {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	body, err := s.storage.Read(ctx, assetURL)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, fmt.Errorf("read asset: %w", err), "error fetching asset").Log(ctx, logger)
	}

	return &gen.ServeChatAttachmentSignedResult{
		ContentType:              row.ContentType,
		ContentLength:            row.ContentLength,
		LastModified:             row.UpdatedAt.Time.Format(time.RFC1123),
		AccessControlAllowOrigin: conv.Ptr("*"),
	}, body, nil
}
