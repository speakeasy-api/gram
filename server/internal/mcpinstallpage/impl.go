package mcpinstallpage

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_install_page/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_install_page"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpinstallpage/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	repo        *repo.Queries
	toolsetRepo *toolsets_repo.Queries
	sessions    *sessions.Manager
	auth        *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("mcp_install_page"))

	return &Service{
		tracer:      otel.Tracer("github.com/speakeasy-api/gram/server/internal/mcpinstallpage"),
		logger:      logger,
		db:          db,
		repo:        repo.New(db),
		toolsetRepo: toolsets_repo.New(db),
		sessions:    sessions,
		auth:        auth.New(logger, db, sessions),
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

func (s *Service) GetInstallPageMetadata(ctx context.Context, payload *gen.GetInstallPageMetadataPayload) (*gen.GetInstallPageMetadataResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeForbidden)
	}

	toolset, err := s.toolsetRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, err, "toolset not found").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch toolset").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	}

	record, err := s.repo.GetMetadataForToolset(ctx, toolset.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "no MCP install page metadata for this toolset").Log(ctx, s.logger, attr.SlogToolsetID(toolset.ID.String()))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch MCP install page metadata").Log(ctx, s.logger)
	}

	return &gen.GetInstallPageMetadataResult{
		Metadata: toInstallPageMetadata(record),
	}, nil
}

func (s *Service) SetInstallPageMetadata(ctx context.Context, payload *gen.SetInstallPageMetadataPayload) (*types.MCPInstallPageMetadata, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeForbidden)
	}

	toolset, err := s.toolsetRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, err, "toolset not found").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch toolset").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	}

	var logoID uuid.NullUUID
	if payload.LogoAssetID != nil {
		parsedLogoID, err := uuid.Parse(*payload.LogoAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset ID").Log(ctx, s.logger)
		}
		logoID = uuid.NullUUID{UUID: parsedLogoID, Valid: true}
	}

	externalDocURL := conv.ToPGText("")
	if payload.ExternalDocumentationURL != nil {
		externalDocURL = conv.ToPGText(*payload.ExternalDocumentationURL)
	}

	result, err := s.repo.UpsertMetadata(ctx, repo.UpsertMetadataParams{
		ToolsetID:                toolset.ID,
		ExternalDocumentationUrl: externalDocURL,
		LogoID:                   logoID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert MCP install page metadata").Log(ctx, s.logger)
	}

	return toInstallPageMetadata(result), nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func toInstallPageMetadata(record repo.McpInstallPageMetadatum) *types.MCPInstallPageMetadata {
	metadata := &types.MCPInstallPageMetadata{
		ID:                       record.ID.String(),
		ToolsetID:                record.ToolsetID.String(),
		CreatedAt:                record.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                record.UpdatedAt.Time.Format(time.RFC3339),
		ExternalDocumentationURL: conv.FromPGText[string](record.ExternalDocumentationUrl),
		LogoAssetID:              conv.FromNullableUUID(record.LogoID),
	}
	return metadata
}
