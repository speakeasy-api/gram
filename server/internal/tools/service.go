package tools

import (
	"context"
	"errors"
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
	"github.com/speakeasy-api/gram/internal/tools/repo"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = &Service{}

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
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) ListTools(ctx context.Context, payload *gen.ListToolsPayload) (res *gen.ListToolsResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID is required")
	}

	params := repo.ListAllHttpToolDefinitionsParams{
		ProjectID: *authCtx.ProjectID,
	}

	if payload.Cursor != nil {
		params.Cursor = uuid.NullUUID{UUID: uuid.MustParse(*payload.Cursor), Valid: true}
	}

	tools, err := s.repo.ListAllHttpToolDefinitions(ctx, params)
	if err != nil {
		return nil, err
	}

	result := &gen.ListToolsResult{
		Tools: make([]*gen.HTTPToolDefinition, len(tools)),
	}

	for i, tool := range tools {
		result.Tools[i] = &gen.HTTPToolDefinition{
			ID:                  tool.ID.String(),
			ProjectID:           tool.ProjectID.String(),
			DeploymentID:        tool.DeploymentID.String(),
			Openapiv3DocumentID: conv.FromNullableUUID(tool.Openapiv3DocumentID),
			Name:                tool.Name,
			Summary:             tool.Summary,
			Description:         tool.Description,
			Openapiv3Operation:  conv.FromPGText(tool.Openapiv3Operation),
			Tags:                tool.Tags,
			Security:            conv.FromBytes(tool.Security),
			HTTPMethod:          tool.HttpMethod,
			Path:                tool.Path,
			SchemaVersion:       &tool.SchemaVersion,
			Schema:              string(tool.Schema),
			CreatedAt:           tool.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:           tool.UpdatedAt.Time.Format(time.RFC3339),
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
