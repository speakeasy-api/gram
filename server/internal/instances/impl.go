package instances

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/instances/server"
	gen "github.com/speakeasy-api/gram/server/gen/instances"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/environments"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usage/types"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

const tooldIdQueryParam = "tool_id"
const environmentSlugQueryParam = "environment_slug"

type Service struct {
	logger           *slog.Logger
	tracer           trace.Tracer
	db               *pgxpool.Pool
	auth             *auth.Auth
	toolset          *toolsets.Toolsets
	environmentsRepo *environments_repo.Queries
	env              *environments.EnvironmentEntries
	toolProxy        *gateway.ToolProxy
	posthog          *posthog.Posthog
	usageClient      usage_types.UsageClient
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	traceProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	env *environments.EnvironmentEntries,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	posthog *posthog.Posthog,
	usageClient usage_types.UsageClient,
) *Service {
	envRepo := environments_repo.New(db)
	tracer := traceProvider.Tracer("github.com/speakeasy-api/gram/server/internal/instances")
	logger = logger.With(attr.SlogComponent("instances"))

	return &Service{
		logger:           logger,
		tracer:           tracer,
		db:               db,
		auth:             auth.New(logger, db, sessions),
		toolset:          toolsets.NewToolsets(db),
		environmentsRepo: envRepo,
		env:              env,
		posthog:          posthog,
		usageClient:      usageClient,
		toolProxy: gateway.NewToolProxy(
			logger,
			traceProvider,
			meterProvider,
			gateway.ToolCallSourceDirect,
			cacheImpl,
			guardianPolicy,
		),
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
	o11y.AttachHandler(mux, "POST", "/rpc/instances.invoke/tool", func(w http.ResponseWriter, r *http.Request) {
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

	environmentEntries, err := s.env.ListEnvironmentEntries(ctx, *authCtx.ProjectID, envModel.ID, true)
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

	promptTemplates := make([]*types.PromptTemplate, len(toolset.PromptTemplates))
	for i, template := range toolset.PromptTemplates {
		promptTemplates[i] = &types.PromptTemplate{
			ID:            template.ID,
			Name:          template.Name,
			HistoryID:     template.HistoryID,
			PredecessorID: template.PredecessorID,
			Prompt:        template.Prompt,
			Description:   template.Description,
			Arguments:     template.Arguments,
			Engine:        template.Engine,
			Kind:          template.Kind,
			ToolsHint:     template.ToolsHint,
			CreatedAt:     template.CreatedAt,
			UpdatedAt:     template.UpdatedAt,
		}
	}

	return &gen.GetInstanceResult{
		Name:              toolset.Name,
		Description:       toolset.Description,
		SecurityVariables: toolset.SecurityVariables,
		ServerVariables:   toolset.ServerVariables,
		Tools:             toolset.HTTPTools,
		PromptTemplates:   promptTemplates,
		Environment:       environment,
	}, nil
}

func (s *Service) ExecuteInstanceTool(w http.ResponseWriter, r *http.Request) error {
	logger := s.logger

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
			return oops.E(oops.CodeUnauthorized, err, "failed to authorize").Log(ctx, logger)
		}
	}

	sc = security.APIKeyScheme{
		Name:           auth.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}

	ctx, err = s.auth.Authorize(ctx, r.Header.Get(auth.ProjectHeader), &sc)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to authorize").Log(ctx, logger)
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "project ID is required").Log(ctx, logger)
	}

	toolID := r.URL.Query().Get(tooldIdQueryParam)
	if toolID == "" {
		return oops.E(oops.CodeBadRequest, nil, "tool_id query parameter is required").Log(ctx, logger)
	}

	// These variabels will come in our playground chats
	toolsetSlug := r.URL.Query().Get("toolset_slug")
	chatID := r.URL.Query().Get("chat_id")

	envVars := make(map[string]string)
	if environmentSlug := r.URL.Query().Get(environmentSlugQueryParam); environmentSlug != "" {
		envModel, err := s.environmentsRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      strings.ToLower(environmentSlug),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, logger)
		}

		environmentEntries, err := s.env.ListEnvironmentEntries(ctx, *authCtx.ProjectID, envModel.ID, false)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load environment entries").Log(ctx, logger)
		}

		// Transform environment entries into a map
		for _, entry := range environmentEntries {
			envVars[entry.Name] = entry.Value
		}
	}

	executionInfo, err := s.toolset.GetHTTPToolExecutionInfoByID(ctx, uuid.MustParse(toolID), *authCtx.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load tool execution info").Log(ctx, logger)
	}
	ctx, logger = o11y.EnrichToolCallContext(ctx, logger, executionInfo.OrganizationSlug, executionInfo.ProjectSlug)

	if chatID != "" && toolsetSlug != "" {
		if err := s.posthog.CaptureEvent(ctx, "direct_tool_execution", authCtx.ProjectID.String(), map[string]interface{}{
			"project_id":          authCtx.ProjectID.String(),
			"project_slug":        authCtx.ProjectSlug,
			"organization_id":     executionInfo.Tool.OrganizationID,
			"organization_slug":   executionInfo.OrganizationSlug,
			"authenticated":       true,
			"toolset_slug":        toolsetSlug,
			"chat_session_id":     chatID,
			"tool_name":           executionInfo.Tool.Name,
			"tool_id":             executionInfo.Tool.ID,
			"disable_noification": true,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture direct_tool_execution event", attr.SlogError(err))
		}
	}

	requestBody := r.Body
	requestBodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read request body").Log(ctx, logger)
	}

	requestNumBytes := int64(len(requestBodyBytes))

	requestBody = io.NopCloser(bytes.NewBuffer(requestBodyBytes))

	// Use a response interceptor that completely captures the response
	interceptor := newResponseInterceptor(w)

	err = s.toolProxy.Do(ctx, interceptor, requestBody, envVars, executionInfo.Tool)
	if err != nil {
		return fmt.Errorf("failed to proxy tool call: %w", err)
	}

	// Write the modified response to the original response writer
	w.WriteHeader(interceptor.statusCode)
	for k, v := range interceptor.headers {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}

	for _, cookie := range interceptor.cookies {
		http.SetCookie(w, cookie)
	}

	_, err = w.Write(interceptor.buffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	// Capture the usage for billing purposes (async to not block response)
	outputNumBytes := int64(interceptor.buffer.Len())

	go s.usageClient.TrackToolCallUsage(context.Background(), usage_types.ToolCallUsageEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		RequestBytes:     requestNumBytes,
		OutputBytes:      outputNumBytes,
		ToolID:           toolID,
		ToolName:         executionInfo.Tool.Name,
		ProjectID:        authCtx.ProjectID.String(),
		ProjectSlug:      authCtx.ProjectSlug,
		Type:             usage_types.ToolCallType_HTTP,
		OrganizationSlug: &executionInfo.OrganizationSlug,
		ToolsetSlug:      &toolsetSlug,
		ChatID:           &chatID,
		MCPURL:           nil, // Not applicable for direct tool calls
	})

	return nil
}

// ResponseInterceptor completely intercepts the response, allowing modifications before sending to client
type responseInterceptor struct {
	http.ResponseWriter
	statusCode int
	buffer     *bytes.Buffer
	headers    http.Header
	cookies    []*http.Cookie
}

var _ http.ResponseWriter = (*responseInterceptor)(nil)

func newResponseInterceptor(w http.ResponseWriter) *responseInterceptor {
	return &responseInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default status code
		buffer:         &bytes.Buffer{},
		headers:        make(http.Header),
		cookies:        make([]*http.Cookie, 0),
	}
}

func (w *responseInterceptor) Header() http.Header {
	return w.headers
}

func (w *responseInterceptor) SetCookie(cookie *http.Cookie) {
	w.cookies = append(w.cookies, cookie)
}

func (w *responseInterceptor) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	// Don't write the header to the underlying ResponseWriter yet
	// We'll do that after we've modified the response
}

// TODO: if we support tool streaming, we will need to handle that here
func (w *responseInterceptor) Write(b []byte) (int, error) {
	n, err := w.buffer.Write(b)
	if err != nil {
		return n, fmt.Errorf("write response body error: %w", err)
	}

	return n, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
