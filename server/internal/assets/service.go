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
	"os"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/assets"
	srv "github.com/speakeasy-api/gram/gen/http/assets/server"
	"github.com/speakeasy-api/gram/internal/assets/repo"
	"github.com/speakeasy-api/gram/internal/auth"
	projectsRepo "github.com/speakeasy-api/gram/internal/projects/repo"
	"github.com/speakeasy-api/gram/internal/sessions"
)

type Service struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Sessions
	storage  BlobStore

	projects *projectsRepo.Queries
	repo     *repo.Queries
}

var _ gen.Service = &Service{}
var _ gen.Auther = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, storage BlobStore) *Service {
	return &Service{
		logger:   logger,
		db:       db,
		sessions: sessions.New(logger),
		storage:  storage,
		projects: projectsRepo.New(db),
		repo:     repo.New(db),
	}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}

func (s *Service) UploadOpenAPIv3(ctx context.Context, payload *gen.UploadOpenAPIv3Payload, reader io.ReadCloser) (*gen.UploadOpenAPIv3Result, error) {
	defer reader.Close()

	access, err := auth.EnsureProjectAccess(ctx, s.logger, s.db, payload.ProjectSlug)
	if err != nil {
		return nil, err
	}

	if payload.ContentLength == 0 {
		return nil, fmt.Errorf("no content")
	}

	if payload.ContentLength > 8*1024*1024 {
		return nil, fmt.Errorf("content length exceeds 8 MiB limit: %d", payload.ContentLength)
	}

	f, err := os.CreateTemp("", "asset-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(f.Name())

	bsize := 4096
	if payload.ContentLength < 4096 {
		bsize = int(payload.ContentLength)
	}
	hash := sha256.New()
	writer := bufio.NewWriterSize(io.MultiWriter(f, hash), bsize)
	_, err = io.Copy(writer, io.LimitReader(reader, payload.ContentLength))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("copy to temp file: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("flush writer: %w", err)
	}

	sha := hex.EncodeToString(hash.Sum(nil))

	asset, err := s.repo.GetProjectAssetBySHA256(ctx, repo.GetProjectAssetBySHA256Params{
		ProjectID: access.ProjectID,
		Sha256:    sha,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find project asset by sha256: %w", err)
	}
	if asset.ID != uuid.Nil {
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

	filename := fmt.Sprintf("openapi-%s", sha)

	contentType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, fmt.Errorf("parse content type: %w", err)
	}

	switch contentType {
	case "application/yaml":
		filename += ".yaml"
	case "application/json":
		filename += ".json"
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	off, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("seek to start: %w", err)
	}
	if off != 0 {
		return nil, fmt.Errorf("seek to start: offset not 0: %d", off)
	}

	dst, uri, err := s.storage.Write(ctx, path.Join(access.ProjectID.String(), filename), f, contentType)
	if err != nil {
		return nil, fmt.Errorf("write to blob storage: %w", err)
	}
	defer dst.Close()

	n, err := io.CopyBuffer(dst, f, make([]byte, bsize))
	if err != nil {
		return nil, fmt.Errorf("copy to blob storage: %w", err)
	}
	if n != payload.ContentLength {
		return nil, fmt.Errorf("expected %d bytes, wrote %d", payload.ContentLength, n)
	}

	if err := dst.Close(); err != nil {
		return nil, fmt.Errorf("finalize blob storage: %w", err)
	}

	asset, err = s.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     access.ProjectID,
		Sha256:        sha,
		Kind:          "openapiv3",
		ContentType:   contentType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		return nil, fmt.Errorf("create asset in database: %w", err)
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
