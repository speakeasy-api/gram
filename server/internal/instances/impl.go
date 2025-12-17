package instances

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	tm_repo  "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const toolUrnQueryParam = "tool_urn"
const environmentSlugQueryParam = "environment_slug"

type Service struct {
	logger            *slog.Logger
	tracer            trace.Tracer
	db                *pgxpool.Pool
	auth              *auth.Auth
	toolset           *toolsets.Toolsets
	environmentsRepo  *environments_repo.Queries
	env               *environments.EnvironmentEntries
	toolProxy         *gateway.ToolProxy
	tracking          billing.Tracker
	toolsetCache      cache.TypedCacheObject[mv.ToolsetBaseContents]
	tcm               tm.ToolMetricsProvider
	customDomainsRepo *customdomainsRepo.Queries
	serverURL         *url.URL
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	traceProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	env *environments.EnvironmentEntries,
	enc *encryption.Client,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	funcCaller functions.ToolCaller,
	tracking billing.Tracker,
	tcm tm.ToolMetricsProvider,
	serverURL *url.URL,
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
		tracking:         tracking,
		toolProxy: gateway.NewToolProxy(
			logger,
			traceProvider,
			meterProvider,
			gateway.ToolCallSourceDirect,
			enc,
			cacheImpl,
			guardianPolicy,
			funcCaller,
		),
		toolsetCache:      cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
		tcm:               tcm,
		customDomainsRepo: customdomainsRepo.New(db),
		serverURL:         serverURL,
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

	toolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(conv.ToLower(payload.ToolsetSlug)), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	promptTemplates := make([]*types.PromptTemplate, len(toolset.PromptTemplates))
	for i, template := range toolset.PromptTemplates {
		promptTemplates[i] = &types.PromptTemplate{
			ID:            template.ID,
			ProjectID:     template.ProjectID,
			ToolUrn:       template.ToolUrn,
			Name:          template.Name,
			HistoryID:     template.HistoryID,
			PredecessorID: template.PredecessorID,
			Prompt:        template.Prompt,
			Description:   template.Description,
			Schema:        template.Schema,
			SchemaVersion: template.SchemaVersion,
			Engine:        template.Engine,
			Kind:          template.Kind,
			ToolsHint:     template.ToolsHint,
			ToolUrnsHint:  template.ToolUrnsHint,
			CreatedAt:     template.CreatedAt,
			UpdatedAt:     template.UpdatedAt,
			CanonicalName: template.CanonicalName,
			Confirm:       template.Confirm,
			ConfirmPrompt: template.ConfirmPrompt,
			Summarizer:    template.Summarizer,
			Canonical:     template.Canonical,
			Variation:     template.Variation,
		}
	}

	baseURL := s.serverURL.String()
	if toolset.CustomDomainID != nil {
		customDomain, err := s.customDomainsRepo.GetCustomDomainByID(ctx, uuid.MustParse(*toolset.CustomDomainID))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get custom domain").Log(ctx, s.logger)
		}
		baseURL = fmt.Sprintf("https://%s", customDomain.Domain)
	}

	// modern gram toolsets always have an MCP slug
	mcpServers := make([]*gen.InstanceMcpServer, 0)
	if toolset.McpSlug != nil {
		mcpServers = append(mcpServers, &gen.InstanceMcpServer{
			URL: fmt.Sprintf("%s/mcp/%s", baseURL, string(*toolset.McpSlug)),
		})
	}

	return &gen.GetInstanceResult{
		Name:                         toolset.Name,
		Description:                  toolset.Description,
		SecurityVariables:            toolset.SecurityVariables,
		ServerVariables:              toolset.ServerVariables,
		FunctionEnvironmentVariables: toolset.FunctionEnvironmentVariables,
		Tools:                        toolset.Tools,
		PromptTemplates:              promptTemplates,
		McpServers:                   mcpServers,
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
			sc := security.APIKeyScheme{
				Name:           auth.KeySecurityScheme,
				RequiredScopes: []string{"chat"},
				Scopes:         []string{},
			}
			ctx, err = s.auth.Authorize(r.Context(), r.Header.Get(auth.APIKeyHeader), &sc)
			if err != nil {
				return oops.E(oops.CodeUnauthorized, err, "failed to authorize").Log(ctx, logger)
			}
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

	toolURNParam := r.URL.Query().Get(toolUrnQueryParam)
	if toolURNParam == "" {
		return oops.E(oops.CodeBadRequest, nil, "tool_urn query parameter is required").Log(ctx, logger)
	}
	toolURN, err := urn.ParseTool(toolURNParam)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse tool URN").Log(ctx, logger)
	}

	// These variables will come in our playground chats
	toolsetSlug := r.URL.Query().Get("toolset_slug")
	chatID := r.URL.Query().Get("chat_id")

	ciEnv := gateway.NewCaseInsensitiveEnv()
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

		for _, entry := range environmentEntries {
			ciEnv.Set(entry.Name, entry.Value)
		}
	}

	plan, err := s.toolset.GetToolCallPlanByURN(ctx, toolURN, *authCtx.ProjectID)
	if err != nil || plan == nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load tool call plan").Log(ctx, logger)
	}

	descriptor := plan.Descriptor
	var toolType tm_repo.ToolType
	switch plan.Kind {
	case gateway.ToolKindHTTP:
		toolType = tm_repo.ToolTypeHTTP
	case gateway.ToolKindFunction:
		toolType = tm_repo.ToolTypeFunction
	case gateway.ToolKindPrompt:
		toolType = tm_repo.ToolTypePrompt
	case gateway.ToolKindExternalMCP:
		return fmt.Errorf("execute external mcp tool from instance: %s", toolURN.String())
	}

	toolCallLogger, logErr := tm.NewToolCallLogger(ctx, s.tcm, descriptor.OrganizationID, tm.ToolInfo{
		ID:             descriptor.ID,
		Urn:            descriptor.URN.String(),
		Name:           descriptor.Name,
		ProjectID:      descriptor.ProjectID,
		DeploymentID:   descriptor.DeploymentID,
		OrganizationID: descriptor.OrganizationID,
	}, descriptor.Name, toolType)
	if logErr != nil {
		logger.ErrorContext(ctx,
			"failed to prepare tool call log entry",
			attr.SlogError(logErr),
			attr.SlogToolName(descriptor.Name),
			attr.SlogToolURN(descriptor.URN.String()),
		)
	}
	ctx, logger = o11y.EnrichToolCallContext(ctx, logger, descriptor.OrganizationSlug, descriptor.ProjectSlug)

	var toolset *types.Toolset
	var toolsetUUID uuid.UUID
	if chatID != "" && toolsetSlug != "" {
		var toolsetErr error
		toolset, toolsetErr = mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolsetSlug), &s.toolsetCache)
		if toolsetErr != nil {
			logger.ErrorContext(ctx, "failed to load toolset", attr.SlogError(err))
		} else if toolset != nil {
			toolsetUUID, err = uuid.Parse(toolset.ID)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to parse toolset ID").Log(ctx, logger)
			}
		}
	}

	systemConfig, err := s.env.LoadSystemEnv(ctx, *authCtx.ProjectID, toolsetUUID, string(toolURN.Kind), toolURN.Source)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load system environment").Log(ctx, logger)
	}

	requestBody := r.Body
	requestBodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read request body").Log(ctx, logger)
	}

	requestNumBytes := int64(len(requestBodyBytes))

	requestBody = io.NopCloser(bytes.NewBuffer(requestBodyBytes))

	interceptor := newResponseInterceptor(w)

	err = s.toolProxy.Do(ctx, interceptor, requestBody, gateway.ToolCallEnv{
		SystemEnv:  systemConfig,
		UserConfig: ciEnv,
	}, plan, toolCallLogger)
	if err != nil {
		return fmt.Errorf("failed to proxy tool call: %w", err)
	}

	// Write the modified response to the original response writer
	for k, v := range interceptor.headers {
		if k == functions.FunctionsCPUHeader || k == functions.FunctionsMemoryHeader {
			continue
		}
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}

	for _, cookie := range interceptor.cookies {
		http.SetCookie(w, cookie)
	}

	w.WriteHeader(interceptor.statusCode)

	_, err = w.Write(interceptor.buffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	// Capture the usage for billing purposes (async to not block response)
	outputNumBytes := int64(interceptor.buffer.Len())

	// Extract function metrics from headers (originally trailers from functions runner)
	var functionCPU *float64
	var functionMem *float64
	var functionsExecutionTime *float64
	if cpuStr := interceptor.headers.Get(functions.FunctionsCPUHeader); cpuStr != "" {
		if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
			functionCPU = &cpu
		}
	}
	if memStr := interceptor.headers.Get(functions.FunctionsMemoryHeader); memStr != "" {
		if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
			functionMem = &mem
		}
	}
	if execTimeStr := interceptor.headers.Get(functions.FunctionsExecutionTimeHeader); execTimeStr != "" {
		if execTime, err := strconv.ParseFloat(execTimeStr, 64); err == nil {
			functionsExecutionTime = &execTime
		}
	}

	organizationID := authCtx.ActiveOrganizationID
	if organizationID == "" && toolset != nil {
		organizationID = toolset.OrganizationID
	}

	var toolsetID *string
	if toolset != nil {
		toolsetID = &toolset.ID
	}
	toolName := descriptor.Name

	// emit logs to tool metrics system, will only do so if enabled
	defer func() {
		go s.tracking.TrackToolCallUsage(context.WithoutCancel(ctx), billing.ToolCallUsageEvent{
			OrganizationID:        organizationID,
			RequestBytes:          requestNumBytes,
			OutputBytes:           outputNumBytes,
			ToolURN:               toolURN.String(),
			ToolName:              toolName,
			ProjectID:             authCtx.ProjectID.String(),
			ProjectSlug:           authCtx.ProjectSlug,
			Type:                  plan.BillingType,
			OrganizationSlug:      &descriptor.OrganizationSlug,
			ToolsetSlug:           &toolsetSlug,
			ChatID:                &chatID,
			ToolsetID:             toolsetID,
			ResponseStatusCode:    interceptor.statusCode,
			MCPURL:                nil, // Not applicable for direct tool calls
			MCPSessionID:          nil, // Not applicable for direct tool calls
			ResourceURI:           "",
			FunctionCPUUsage:      functionCPU,
			FunctionMemUsage:      functionMem,
			FunctionExecutionTime: functionsExecutionTime,
		})

		toolCallLogger.RecordStatusCode(interceptor.statusCode)
		toolCallLogger.RecordRequestBodyBytes(requestNumBytes)
		toolCallLogger.RecordResponseBodyBytes(outputNumBytes)
		toolCallLogger.Emit(context.WithoutCancel(ctx), logger)
	}()

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
