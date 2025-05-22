package instances

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
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
	chatClient       *openrouter.ChatClient
}

var _ gen.Service = (*Service)(nil)

const SUMMARIZE_BREAKPOINT_BYTES = 4 * 25_000 // 4 bytes per token, 25k token limit

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption, chatClient *openrouter.ChatClient) *Service {
	envRepo := environments_repo.New(db)
	return &Service{
		tracer:           otel.Tracer("github.com/speakeasy-api/gram/internal/instances"),
		logger:           logger,
		db:               db,
		auth:             auth.New(logger, db, sessions),
		toolset:          toolsets.NewToolsets(db),
		environmentsRepo: envRepo,
		entries:          environments.NewEnvironmentEntries(logger, envRepo, enc),
		chatClient:       chatClient,
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
			Canonical:           tool.Canonical,
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

	requestBody := r.Body
	requestBodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read request body").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "request body", slog.String("request_body", string(requestBodyBytes)))

	requestBody = io.NopCloser(bytes.NewBuffer(requestBodyBytes))

	// Use a response interceptor that completely captures the response
	// This is so we can modify the response body before writing it to the original writer
	// in the event summarization is needed
	interceptor := newResponseInterceptor(w)

	// Transform environment entries into a map
	envVars := make(map[string]string)
	for _, entry := range environmentEntries {
		envVars[entry.Name] = entry.Value
	}

	err = InstanceToolProxy(ctx, s.tracer, s.logger, interceptor, requestBody, envVars, executionInfo)
	if err != nil {
		return err
	}

	// The original, unmodified response body
	responseBody := interceptor.buffer.String()

	// Summarize if the response is too large or if the "gram-request-summary" param is provided
	shouldSummarize := len(responseBody) > SUMMARIZE_BREAKPOINT_BYTES
	var requestSummary struct {
		GramRequestSummary string `json:"gram-request-summary"`
	}
	err = json.Unmarshal(requestBodyBytes, &requestSummary)
	if err == nil && requestSummary.GramRequestSummary != "" {
		shouldSummarize = true
	}

	if shouldSummarize {
		summarizedResponse, err := s.summarizeResponse(ctx, authCtx.ActiveOrganizationID, requestSummary.GramRequestSummary, responseBody)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to summarize response").Log(ctx, s.logger)
		}

		s.logger.InfoContext(ctx, "summarizing response",
			slog.Int("original_length", len(responseBody)),
			slog.Int("summarized_length", len(summarizedResponse)))

		responseBody = summarizedResponse
	}

	// Write the modified response to the original response writer
	w.WriteHeader(interceptor.statusCode)
	for k, v := range interceptor.headers {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}

	_, err = w.Write([]byte(responseBody))
	return err
}

func (s *Service) summarizeResponse(ctx context.Context, orgID string, requestSummary, responseBody string) (string, error) {
	systemPrompt := `
	You are a helper tasked with reducing the size of the response of a tool based on a given request summary.
	For example, if the request is to "List all usernames", the response may include a list of users with many additional fields not relevant to the request.
	Your goal is to extract the information that is most relevant to the request summary and return it as a new response.
	There's no need to over-summarize responses that are already concise. Prioritize reducing enormous responses to manageable sizes.
	`
	prompt := fmt.Sprintf("Here is the request summary:\n\n%s\n\nHere is the response body:\n\n%s", requestSummary, responseBody)
	chatResponse, err := s.chatClient.GetCompletion(ctx, orgID, systemPrompt, prompt, nil)
	if err != nil {
		s.logger.ErrorContext(ctx, "error getting chat response", slog.String("error", err.Error()))
		return "", err
	}

	return chatResponse.Content, nil
}

// ResponseInterceptor completely intercepts the response, allowing modifications before sending to client
type responseInterceptor struct {
	http.ResponseWriter
	statusCode int
	buffer     *bytes.Buffer
	headers    http.Header
}

var _ http.ResponseWriter = (*responseInterceptor)(nil)

func newResponseInterceptor(w http.ResponseWriter) *responseInterceptor {
	return &responseInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default status code
		buffer:         &bytes.Buffer{},
		headers:        make(http.Header),
	}
}

func (w *responseInterceptor) Header() http.Header {
	return w.headers
}

func (w *responseInterceptor) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	// Don't write the header to the underlying ResponseWriter yet
	// We'll do that after we've modified the response
}

// TODO: if we support tool streaming, we will need to handle that here
func (w *responseInterceptor) Write(b []byte) (int, error) {
	return w.buffer.Write(b)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
