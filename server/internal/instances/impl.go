package instances

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/instances/server"
	gen "github.com/speakeasy-api/gram/gen/instances"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments"
	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/oops"
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
	entries          *environments.EnvironmentEntries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption) *Service {
	envRepo := environments_repo.New(db)
	return &Service{
		tracer:           otel.Tracer("github.com/speakeasy-api/gram/internal/instances"),
		logger:           logger,
		db:               db,
		auth:             auth.New(logger, db, sessions),
		toolset:          toolsets.NewToolsets(db),
		environmentsRepo: envRepo,
		entries:          environments.NewEnvironmentEntries(logger, envRepo, enc),
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
	mux.Handle("POST", "/rpc/instances.invoke/tool", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ExecuteInstanceTool).ServeHTTP(w, r)
	})
}

func (s *Service) GetInstance(ctx context.Context, payload *gen.GetInstanceForm) (res *gen.GetInstanceResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	toolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(conv.ToLower(payload.ToolsetSlug)))
	if err != nil {
		return nil, err
	}

	if toolset.DefaultEnvironmentSlug == nil && payload.EnvironmentSlug == nil {
		return nil, oops.E(oops.CodeInvalid, nil, "environment is required").Log(ctx, s.logger)
	}

	var envModel environments_repo.Environment
	if payload.EnvironmentSlug != nil {
		envModel, err = s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      conv.ToLower(*payload.EnvironmentSlug),
		})
	} else {
		envModel, err = s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      string(*toolset.DefaultEnvironmentSlug),
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, s.logger)
	}

	environmentEntries, err := s.entries.ListEnvironmentEntries(ctx, envModel.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment entries").Log(ctx, s.logger)
	}

	genEntries := make([]*types.EnvironmentEntry, len(environmentEntries))
	for i, entry := range environmentEntries {
		genEntries[i] = &types.EnvironmentEntry{
			Name:      entry.Name,
			Value:     entry.Value,
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	environment := &types.Environment{
		ID:             envModel.ID.String(),
		OrganizationID: envModel.OrganizationID,
		ProjectID:      envModel.ProjectID.String(),
		Name:           envModel.Name,
		Slug:           types.Slug(envModel.Slug),
		Description:    conv.FromPGText[string](envModel.Description),
		Entries:        genEntries,
		CreatedAt:      envModel.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      envModel.UpdatedAt.Time.Format(time.RFC3339),
	}

	httpTools := make([]*types.HTTPToolDefinition, len(toolset.HTTPTools))
	for i, tool := range toolset.HTTPTools {
		httpTools[i] = &types.HTTPToolDefinition{
			ID:                  tool.ID,
			ProjectID:           tool.ProjectID,
			DeploymentID:        tool.DeploymentID,
			Openapiv3DocumentID: tool.Openapiv3DocumentID,
			Name:                tool.Name,
			Summary:             tool.Summary,
			Description:         tool.Description,
			Confirm:             tool.Confirm,
			ConfirmPrompt:       tool.ConfirmPrompt,
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

	return &gen.GetInstanceResult{
		Name:                         toolset.Name,
		Description:                  toolset.Description,
		RelevantEnvironmentVariables: toolset.RelevantEnvironmentVariables,
		Tools:                        httpTools,
		Environment:                  environment,
	}, nil
}

func (s *Service) ExecuteInstanceTool(w http.ResponseWriter, r *http.Request) error {
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
			Scopes:         []string{},
		}
		ctx, err = s.auth.Authorize(r.Context(), r.Header.Get(auth.APIKeyHeader), &sc)
		if err != nil {
			return oops.E(oops.CodeUnauthorized, err, "failed to authorize").Log(ctx, s.logger)
		}
	}
	sc = security.APIKeyScheme{
		Name:           auth.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err = s.auth.Authorize(ctx, r.Header.Get(auth.ProjectHeader), &sc)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to authorize").Log(ctx, s.logger)
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "project ID is required").Log(ctx, s.logger)
	}

	toolID := r.URL.Query().Get(tooldIdQueryParam)
	if toolID == "" {
		return oops.E(oops.CodeBadRequest, nil, "tool_id query parameter is required").Log(ctx, s.logger)
	}

	environmentSlug := r.URL.Query().Get(environmentSlugQueryParam)
	if environmentSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "environment_slug query parameter is required").Log(ctx, s.logger)
	}

	envModel, err := s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      strings.ToLower(environmentSlug),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, s.logger)
	}

	environmentEntries, err := s.entries.ListEnvironmentEntries(ctx, envModel.ID, false)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load environment entries").Log(ctx, s.logger)
	}

	executionInfo, err := s.toolset.GetHTTPToolExecutionInfoByID(ctx, uuid.MustParse(toolID), *authCtx.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load tool execution info").Log(ctx, s.logger)
	}

	return InstanceToolProxy(ctx, s.tracer, s.logger, w, r.Body, environmentEntries, executionInfo)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
