package instances

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/instances/server"
	gen "github.com/speakeasy-api/gram/gen/instances"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

const tooldIdQueryParam = "tool_id"
const environmentSlugQueryParam = "environment_slug"

type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	db               *pgxpool.Pool
	auth             *auth.Auth
	toolset          *toolsets.Toolsets
	environmentsRepo *environments_repo.Queries
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Sessions) *Service {
	return &Service{
		tracer:           otel.Tracer("github.com/speakeasy-api/gram/internal/instances"),
		logger:           logger,
		db:               db,
		auth:             auth.New(logger, db, sessions),
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
	mux.Handle("POST", "/rpc/instances.invoke/tool", func(w http.ResponseWriter, r *http.Request) {
		service.ExecuteInstanceTool(w, r)
	})
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

	if toolset.DefaultEnvironmentSlug == nil && payload.EnvironmentSlug == nil {
		return nil, errors.New("an environment must be provided to use this toolset")
	}

	var envModel environments_repo.Environment
	if payload.EnvironmentSlug != nil {
		envModel, err = s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      *payload.EnvironmentSlug,
		})
	} else {
		envModel, err = s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      *toolset.DefaultEnvironmentSlug,
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
			Value:     conv.RedactedEnvironment(entry.Value),
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
			ID:                  tool.ID,
			ProjectID:           tool.ProjectID,
			DeploymentID:        tool.DeploymentID,
			Openapiv3DocumentID: tool.Openapiv3DocumentID,
			Name:                tool.Name,
			Summary:             tool.Summary,
			Description:         tool.Description,
			Openapiv3Operation:  tool.Openapiv3Operation,
			Tags:                tool.Tags,
			Security:            tool.Security,
			HTTPMethod:          tool.HTTPMethod,
			Path:                tool.Path,
			SchemaVersion:       tool.SchemaVersion,
			Schema:              tool.Schema,
			CreatedAt:           tool.CreatedAt,
			UpdatedAt:           tool.UpdatedAt,
		}
	}

	return &gen.InstanceResult{
		Name:                         toolset.Name,
		Description:                  toolset.Description,
		RelevantEnvironmentVariables: toolset.RelevantEnvironmentVariables,
		Tools:                        httpTools,
		Environment:                  environment,
	}, nil
}

func (s *Service) ExecuteInstanceTool(w http.ResponseWriter, r *http.Request) {
	// TODO: Handling security, we can probably factor this out into something smarter like a proxy
	sc := security.APIKeyScheme{
		Name:           auth.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err := s.auth.Authorize(r.Context(), r.Header.Get(auth.SessionHeader), &sc)
	if err != nil {
		sc := security.APIKeyScheme{
			Name:           auth.KeySecurityScheme,
			RequiredScopes: []string{"consumer"},
		}
		ctx, err = s.auth.Authorize(r.Context(), r.Header.Get(auth.APIKeyHeader), &sc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	}
	sc = security.APIKeyScheme{
		Name: auth.ProjectSlugSecuritySchema,
	}
	ctx, err = s.auth.Authorize(ctx, r.Header.Get(auth.ProjectHeader), &sc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		http.Error(w, "project ID is required", http.StatusUnauthorized)
		return
	}

	toolID := r.URL.Query().Get(tooldIdQueryParam)
	if toolID == "" {
		http.Error(w, "tool_id query parameter is required", http.StatusBadRequest)
		return
	}

	environmentSlug := r.URL.Query().Get(environmentSlugQueryParam)
	if environmentSlug == "" {
		http.Error(w, "environment_slug query parameter is required", http.StatusBadRequest)
		return
	}

	envModel, err := s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      environmentSlug,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	environmentEntries, err := s.environmentsRepo.ListEnvironmentEntries(ctx, envModel.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	executionInfo, err := s.toolset.GetHTTPToolExecutionInfoByID(ctx, uuid.MustParse(toolID), *authCtx.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	InstanceToolProxy(ctx, s.tracer, s.logger, w, r, environmentEntries, executionInfo)
	return
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
