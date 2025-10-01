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
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	sessions *sessions.Manager
	auth     *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("mcp_install_page"))

	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/server/internal/mcpinstallpage"),
		logger:   logger,
		db:       db,
		repo:     repo.New(db),
		sessions: sessions,
		auth:     auth.New(logger, db, sessions),
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

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset ID").Log(ctx, s.logger)
	}

	if err := s.ensureToolsetOwnership(ctx, *authCtx.ProjectID, toolsetID, authCtx); err != nil {
		return nil, err
	}

	record, err := s.repo.GetMetadataForToolset(ctx, toolsetID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "no MCP install page metadata for this toolset").Log(ctx, s.logger, projectAttrs(authCtx)...)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch MCP install page metadata").Log(ctx, s.logger, projectAttrs(authCtx)...)
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

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset ID").Log(ctx, s.logger)
	}

	if err := s.ensureToolsetOwnership(ctx, *authCtx.ProjectID, toolsetID, authCtx); err != nil {
		return nil, err
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
		ToolsetID:                toolsetID,
		ExternalDocumentationUrl: externalDocURL,
		LogoID:                   logoID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert MCP install page metadata").Log(ctx, s.logger, projectAttrs(authCtx)...)
	}

	return toInstallPageMetadata(result), nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func toInstallPageMetadata(record repo.McpInstallPageMetadatum) *types.MCPInstallPageMetadata {
	metadata := &types.MCPInstallPageMetadata{
		ID:        record.ID.String(),
		ToolsetID: record.ToolsetID.String(),
		CreatedAt: record.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt: record.UpdatedAt.Time.Format(time.RFC3339),
	}

	if url := conv.FromPGText[string](record.ExternalDocumentationUrl); url != nil {
		metadata.ExternalDocumentationURL = url
	}

	metadata.LogoAssetID = conv.FromNullableUUID(record.LogoID)

	return metadata
}

func (s *Service) ensureToolsetOwnership(ctx context.Context, projectID uuid.UUID, toolsetID uuid.UUID, authCtx *contextvalues.AuthContext) error {
	_, err := s.repo.EnsureToolsetOwnership(ctx, repo.EnsureToolsetOwnershipParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger, append(projectAttrs(authCtx), attr.SlogToolsetID(toolsetID.String()))...)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to verify toolset ownership").Log(ctx, s.logger, append(projectAttrs(authCtx), attr.SlogToolsetID(toolsetID.String()))...)
	default:
		return nil
	}
}

func projectAttrs(authCtx *contextvalues.AuthContext) []any {
	attrs := make([]any, 0, 2)
	if authCtx.ProjectSlug != nil {
		attrs = append(attrs, attr.SlogProjectSlug(*authCtx.ProjectSlug))
	}
	if authCtx.ActiveOrganizationID != "" {
		attrs = append(attrs, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}
	return attrs
}
