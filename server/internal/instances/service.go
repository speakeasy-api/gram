package instances

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/instances/server"
	gen "github.com/speakeasy-api/gram/gen/instances"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	toolsets_repo "github.com/speakeasy-api/gram/internal/toolsets/repo"
)

type Service struct {
	logger           *slog.Logger
	db               *pgxpool.Pool
	auth             *auth.Auth
	toolsetsRepo     *toolsets_repo.Queries
	environmentsRepo *environments_repo.Queries
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{logger: logger, db: db, auth: auth.New(logger, db, redisClient), toolsetsRepo: toolsets_repo.New(db), environmentsRepo: environments_repo.New(db)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
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

	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      payload.ToolsetSlug,
	})
	if err != nil {
		return nil, err
	}

	if !toolset.DefaultEnvironmentID.Valid && payload.EnvironmentSlug != nil {
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
			ID:        toolset.DefaultEnvironmentID.UUID,
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
			Value:     entry.Value,
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

	definitions, err := s.toolsetsRepo.GetHTTPToolDefinitions(ctx, toolset.HttpToolIds)
	if err != nil {
		return nil, err
	}

	httpTools := make([]*gen.HTTPToolDefinition, len(definitions))
	for i, def := range definitions {
		httpTools[i] = &gen.HTTPToolDefinition{
			ID:               def.ID.String(),
			Name:             def.Name,
			Description:      def.Description,
			Tags:             def.Tags,
			ServerEnvVar:     def.ServerEnvVar,
			SecurityType:     def.SecurityType,
			BearerEnvVar:     conv.FromPGText(def.BearerEnvVar),
			ApikeyEnvVar:     conv.FromPGText(def.ApikeyEnvVar),
			UsernameEnvVar:   conv.FromPGText(def.UsernameEnvVar),
			PasswordEnvVar:   conv.FromPGText(def.PasswordEnvVar),
			HTTPMethod:       def.HttpMethod,
			Path:             def.Path,
			HeadersSchema:    conv.FromBytes(def.HeadersSchema),
			QueriesSchema:    conv.FromBytes(def.QueriesSchema),
			PathparamsSchema: conv.FromBytes(def.PathparamsSchema),
			BodySchema:       conv.FromBytes(def.BodySchema),
			CreatedAt:        def.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:        def.UpdatedAt.Time.Format(time.RFC3339),
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
