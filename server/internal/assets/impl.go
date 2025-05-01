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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/assets"
	srv "github.com/speakeasy-api/gram/gen/http/assets/server"
	"github.com/speakeasy-api/gram/internal/assets/repo"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/internal/projects/repo"
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
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/internal/assets"),
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) UploadOpenAPIv3(ctx context.Context, payload *gen.UploadOpenAPIv3Form, reader io.ReadCloser) (*gen.UploadOpenAPIv3Result, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if payload.ContentLength == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "no content")
	}

	if payload.ContentLength > 8*1024*1024 {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length exceeds 8 MiB limit")
	}

	f, err := os.CreateTemp("", "asset-*")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create temp file: %w", err), "error downloading document")
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return os.Remove(f.Name())
	})

	bsize := 4096
	if payload.ContentLength < 4096 {
		bsize = int(payload.ContentLength)
	}
	hash := sha256.New()
	writer := bufio.NewWriterSize(io.MultiWriter(f, hash), bsize)
	_, err = io.Copy(writer, io.LimitReader(reader, payload.ContentLength))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy to temp file: %w", err), "error downloading document")
	}
	if err := writer.Flush(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("flush writer: %w", err), "error downloading document")
	}

	sha := hex.EncodeToString(hash.Sum(nil))

	asset, err := s.repo.GetProjectAssetBySHA256(ctx, repo.GetProjectAssetBySHA256Params{
		ProjectID: *authCtx.ProjectID,
		Sha256:    sha,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("find project asset by sha256: %w", err), "error loading document data")
	}
	if asset.ID != uuid.Nil {
		if assetURL, err := url.Parse(asset.Url); err == nil {
			exists, err := s.storage.Exists(ctx, assetURL)
			switch {
			case err != nil:
				s.logger.ErrorContext(ctx, "failed to check if asset exists", slog.String("url", asset.Url), slog.String("error", err.Error()))
			case exists:
				return &gen.UploadOpenAPIv3Result{
					Asset: &gen.Asset{
						ID:            asset.ID.String(),
						URL:           asset.Url,
						Kind:          asset.Kind,
						Sha256:        asset.Sha256,
						ContentType:   asset.ContentType,
						ContentLength: asset.ContentLength,
						CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
						UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
					},
				}, nil
			default:
				// it doesn't exist, carry on to create it
			}
		} else {
			s.logger.ErrorContext(ctx, "failed to parse asset url", slog.String("url", asset.Url), slog.String("error", err.Error()))
		}
	}

	filename := fmt.Sprintf("openapi-%s", sha)

	contentType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse content type: %w", err), "error parsing content type")
	}

	switch contentType {
	case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
		filename += ".yaml"
	case "application/json", "text/json":
		filename += ".json"
	default:
		return nil, oops.E(oops.CodeUnsupportedMedia, nil, "unsupported content type: %s", contentType)
	}

	off, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("seek to start: %w", err), "error reading document")
	}
	if off != 0 {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("seek to start: offset not 0: %d", off), "error reading document")
	}

	projectID := *authCtx.ProjectID
	dst, uri, err := s.storage.Write(ctx, path.Join(projectID.String(), filename), f, contentType)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("write to blob storage: %w", err), "error writing document")
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return dst.Close()
	})

	n, err := io.CopyBuffer(dst, f, make([]byte, bsize))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy to blob storage: %w", err), "error uploading document")
	}
	if n != payload.ContentLength {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("expected %d bytes, wrote %d", payload.ContentLength, n), "error uploading document")
	}

	if err := dst.Close(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("finalize blob storage: %w", err), "error uploading document")
	}

	asset, err = s.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     *authCtx.ProjectID,
		Sha256:        sha,
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
			URL:           asset.Url,
			Kind:          asset.Kind,
			Sha256:        asset.Sha256,
			ContentType:   asset.ContentType,
			ContentLength: asset.ContentLength,
			CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
		},
	}, nil
}
