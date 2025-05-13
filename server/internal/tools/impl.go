package tools

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/tools/server"
	gen "github.com/speakeasy-api/gram/gen/tools"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/tools/repo"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/internal/tools"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
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

func (s *Service) ListTools(ctx context.Context, payload *gen.ListToolsPayload) (res *gen.ListToolsResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	params := repo.ListToolsParams{
		ProjectID:    *authCtx.ProjectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
	}

	if payload.Cursor != nil {
		params.Cursor = uuid.NullUUID{UUID: uuid.MustParse(*payload.Cursor), Valid: true}
	}

	if payload.DeploymentID != nil {
		params.DeploymentID = uuid.NullUUID{UUID: uuid.MustParse(*payload.DeploymentID), Valid: true}
	}

	tools, err := s.repo.ListTools(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools").Log(ctx, s.logger)
	}

	result := &gen.ListToolsResult{
		Tools:      make([]*gen.ToolEntry, len(tools)),
		NextCursor: nil,
	}

	for i, tool := range tools {
		var pkg *string
		if tool.PackageName != "" {
			pkg = &tool.PackageName
		}

		result.Tools[i] = &gen.ToolEntry{
			ID:                  tool.ID.String(),
			DeploymentID:        tool.DeploymentID.String(),
			Name:                tool.Name,
			Summary:             tool.Summary,
			Description:         tool.Description,
			Confirm:             conv.PtrValOr(conv.FromPGText[string](tool.Confirm), "always"),
			ConfirmPrompt:       conv.FromPGText[string](tool.ConfirmPrompt),
			HTTPMethod:          tool.HttpMethod,
			Path:                tool.Path,
			Openapiv3DocumentID: tool.Openapiv3DocumentID.UUID.String(),
			PackageName:         pkg,
			CreatedAt:           tool.CreatedAt.Time.Format(time.RFC3339),
		}
	}

	if len(tools) == 100 {
		lastID := tools[len(tools)-1].ID.String()
		result.NextCursor = &lastID
	}

	return result, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
