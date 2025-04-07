package instances

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/instances/server"
	gen "github.com/speakeasy-api/gram/gen/instances"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	db               *pgxpool.Pool
	auth             *auth.Auth
	toolset          *toolsets.Toolsets
	environmentsRepo *environments_repo.Queries
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{
		tracer:           otel.Tracer("github.com/speakeasy-api/gram/internal/instances"),
		logger:           logger,
		db:               db,
		auth:             auth.New(logger, db, redisClient),
		toolset:          toolsets.NewToolsets(db),
		environmentsRepo: environments_repo.New(db),
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

func (s *Service) LoadInstance(ctx context.Context, payload *gen.LoadInstancePayload) (res *gen.InstanceResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID is required")
	}

	toolset, err := s.toolset.LoadToolsetDetails(ctx, payload.ToolsetSlug, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	if toolset.DefaultEnvironmentID == nil && payload.EnvironmentSlug == nil {
		return nil, errors.New("an environment must be provided to use this toolset")
	}

	var envModel environments_repo.Environment
	if payload.EnvironmentSlug != nil {
		envModel, err = s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      *payload.EnvironmentSlug,
		})
	} else {
		envModel, err = s.environmentsRepo.GetEnvironmentByID(ctx, environments_repo.GetEnvironmentByIDParams{
			ProjectID: *authCtx.ProjectID,
			ID:        uuid.MustParse(*toolset.DefaultEnvironmentID),
		})
	}
	if err != nil {
		return nil, err
	}

	environmentEntries, err := s.environmentsRepo.ListEnvironmentEntries(ctx, envModel.ID)
	if err != nil {
		return nil, err
	}

	genEntries := make([]*gen.EnvironmentEntry, len(environmentEntries))
	for i, entry := range environmentEntries {
		genEntries[i] = &gen.EnvironmentEntry{
			Name:      entry.Name,
			Value:     "", // We don't respond with the actual security value on load
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	environment := &gen.Environment{
		ID:             envModel.ID.String(),
		OrganizationID: envModel.OrganizationID,
		ProjectID:      envModel.ProjectID.String(),
		Name:           envModel.Name,
		Slug:           envModel.Slug,
		Description:    conv.FromPGText(envModel.Description),
		Entries:        genEntries,
		CreatedAt:      envModel.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      envModel.UpdatedAt.Time.Format(time.RFC3339),
	}

	httpTools := make([]*gen.HTTPToolDefinition, len(toolset.HTTPTools))
	for i, tool := range toolset.HTTPTools {
		httpTools[i] = &gen.HTTPToolDefinition{
			ID:             tool.ID,
			Name:           tool.Name,
			Description:    tool.Description,
			Tags:           tool.Tags,
			ServerEnvVar:   tool.ServerEnvVar,
			SecurityType:   tool.SecurityType,
			BearerEnvVar:   tool.BearerEnvVar,
			ApikeyEnvVar:   tool.ApikeyEnvVar,
			UsernameEnvVar: tool.UsernameEnvVar,
			PasswordEnvVar: tool.PasswordEnvVar,
			HTTPMethod:     tool.HTTPMethod,
			Path:           tool.Path,
			Schema:         tool.Schema,
			CreatedAt:      tool.CreatedAt,
			UpdatedAt:      tool.UpdatedAt,
		}
	}

	return &gen.InstanceResult{
		Tools:       httpTools,
		Environment: environment,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
