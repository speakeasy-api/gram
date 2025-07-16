package mcp

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/instances"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	meter        metric.Meter
	db           *pgxpool.Pool
	repo         *repo.Queries
	projectsRepo *projects_repo.Queries
	toolsetsRepo *toolsets_repo.Queries
	auth         *auth.Auth
	enc          *encryption.Encryption
	serverURL    *url.URL
	posthog      *posthog.Posthog // posthog metrics will no-op if the dependency is not provided
	toolProxy    *instances.InstanceToolProxy
}

type mcpInputs struct {
	projectID       uuid.UUID
	toolset         string
	environment     string
	mcpEnvVariables map[string]string
	authenticated   bool
}

//go:embed config_snippet.json.tmpl
var configSnippetTmplData string

//go:embed hosted_page.html.tmpl
var hostedPageTmplData string

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption, chatClient *openrouter.ChatClient, posthog *posthog.Posthog, serverURL *url.URL, cacheImpl cache.Cache) *Service {
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcp")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcp")

	return &Service{
		logger:       logger,
		tracer:       tracer,
		meter:        meter,
		db:           db,
		repo:         repo.New(db),
		projectsRepo: projects_repo.New(db),
		toolsetsRepo: toolsets_repo.New(db),
		auth:         auth.New(logger, db, sessions),
		enc:          enc,
		serverURL:    serverURL,
		posthog:      posthog,
		toolProxy: instances.NewInstanceToolProxy(
			logger,
			tracer,
			meter,
			instances.ToolCallSourceMCP,
			cacheImpl,
		),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	mux.Handle("POST", "/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServePublic).ServeHTTP(w, r)
	})
	mux.Handle("GET", "/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
		// This is page is being laoded in the browser equest
		for mediaTypeFull := range strings.SplitSeq(r.Header.Get("Accept"), ",") {
			if mediatype, _, err := mime.ParseMediaType(mediaTypeFull); err == nil && (mediatype == "text/html" || mediatype == "application/xhtml+xml") {
				oops.ErrHandle(service.logger, service.ServeHostedPage).ServeHTTP(w, r)
				return
			}
		}

		body, err := json.Marshal(rpcError{
			ID:      msgID{format: 0, String: "", Number: 0},
			Code:    methodNotAllowed,
			Message: methodNotAllowed.UserMessage(),
			Data:    nil,
		})
		if err != nil {
			service.logger.ErrorContext(r.Context(), "failed to marshal MCP 405 response", slog.String("error", err.Error()))
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, writeErr := w.Write(body)
		if writeErr != nil {
			service.logger.ErrorContext(r.Context(), "failed to write response body", slog.String("error", writeErr.Error()))
		}
	})
	mux.Handle("GET", "/mcp/{mcpSlug}/install", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServeHostedPage).ServeHTTP(w, r)
	})
	mux.Handle("POST", "/mcp/{project}/{toolset}/{environment}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServeAuthenticated).ServeHTTP(w, r)
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

type jsonSnippetData struct {
	MCPName string
	Headers []string
	MCPURL  string
}

type hostedPageData struct {
	jsonSnippetData
	JSONBlobURI string
}

func (s *Service) ServeHostedPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	}

	if !toolset.McpIsPublic {
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to describe toolset")
	}

	envHeaders := []string{}
	for _, envVar := range toolsetDetails.RelevantEnvironmentVariables {
		if !strings.Contains(strings.ToLower(envVar), "server_url") {
			envHeaders = append(envHeaders, fmt.Sprintf("MCP-%s", strings.ReplaceAll(envVar, "_", "-")))
		}
	}

	baseURL := s.serverURL.String() + "/mcp"
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain+"/mcp")
	}
	MCPURL := path.Join(baseURL, mcpSlug)

	configSnippetData := jsonSnippetData{
		MCPName: cases.Title(language.English).String(toolset.Name),
		MCPURL:  MCPURL,
		Headers: envHeaders,
	}

	configSnippetTmpl, err := template.New("config_snippet").Parse(configSnippetTmplData)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to parse config snippet template", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return oops.E(oops.CodeUnexpected, err, "failed to parse config snippet template")
	}

	var configSnippet bytes.Buffer
	if err := configSnippetTmpl.Execute(&configSnippet, configSnippetData); err != nil {
		s.logger.ErrorContext(ctx, "failed to execute config snippet template", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return oops.E(oops.CodeUnexpected, err, "failed to execute config snippet template")
	}

	data := hostedPageData{
		jsonSnippetData: configSnippetData,
		JSONBlobURI:     url.QueryEscape(base64.StdEncoding.EncodeToString(configSnippet.Bytes())),
	}

	hostedPageTmpl, err := template.New("hosted_page").Parse(hostedPageTmplData)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to parse hosted page template", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return oops.E(oops.CodeUnexpected, err, "failed to parse hosted page template")
	}

	if err := hostedPageTmpl.Execute(w, data); err != nil {
		s.logger.ErrorContext(ctx, "failed to execute hosted page template", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return oops.E(oops.CodeUnexpected, err, "failed to execute hosted page template")
	}

	return nil
}

func (s *Service) ServePublic(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	token := r.Header.Get("Authorization")
	if token != "" {
		var err error
		sc := security.APIKeyScheme{
			Name:           auth.KeySecurityScheme,
			RequiredScopes: []string{"consumer"},
			Scopes:         []string{},
		}
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimPrefix(token, "bearer ")
		ctx, err = s.auth.Authorize(ctx, token, &sc)
		if err != nil {
			return oops.C(oops.CodeUnauthorized)
		}
	}

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	}

	var selectedEnvironment string
	var authenticated bool
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID != "" {
		projects, err := s.repo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return oops.E(oops.CodeForbidden, nil, "no projects found")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "error checking project access").Log(ctx, s.logger, slog.String("org_id", authCtx.ActiveOrganizationID))
		}

		projectInOrg := false
		for _, project := range projects {
			if project.ID == toolset.ProjectID {
				projectInOrg = true
				break
			}
		}

		if !projectInOrg {
			return oops.C(oops.CodeUnauthorized)
		}

		authenticated = true
	}

	if !toolset.McpIsPublic && !authenticated {
		return oops.C(oops.CodeUnauthorized)
	}

	// IMPORTANT: We should not use gram environments if we are not in an authenticated context
	if authenticated {
		selectedEnvironment = conv.PtrValOr(conv.FromPGText[string](toolset.DefaultEnvironmentSlug), "")
		if passedEnv := r.Header.Get("Gram-Environment"); passedEnv != "" {
			selectedEnvironment = passedEnv
		}
	}
	var batch batchedRawRequest
	err = json.NewDecoder(r.Body).Decode(&batch)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").Log(ctx, s.logger)
	}

	if len(batch) == 0 {
		return respondWithNoContent(true, w)
	}

	mcpInputs := &mcpInputs{
		projectID:       toolset.ProjectID,
		toolset:         toolset.Slug,
		environment:     selectedEnvironment,
		mcpEnvVariables: parseMcpEnvVariables(r),
		authenticated:   authenticated,
	}

	body, err := s.handleBatch(ctx, mcpInputs, batch)
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		return NewErrorFromCause(batch[0].ID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		s.logger.ErrorContext(ctx, "failed to write response body", slog.String("error", writeErr.Error()))
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}
	return nil
}

func (s *Service) loadToolsetFromMcpSlug(ctx context.Context, mcpSlug string) (*toolsets_repo.Toolset, *contextvalues.CustomDomainContext, error) {
	var toolset toolsets_repo.Toolset
	var toolsetErr error
	var customDomainCtx *contextvalues.CustomDomainContext
	if domainCtx, ok := contextvalues.GetCustomDomainContext(ctx); ok && domainCtx != nil {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true},
		})
		customDomainCtx = domainCtx
	} else {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug)) //
	}

	if toolsetErr != nil {
		s.logger.ErrorContext(ctx, "failed to get toolset for MCP server slug", slog.String("error", toolsetErr.Error()))
		return nil, nil, oops.E(oops.CodeNotFound, toolsetErr, "mcp server not found").Log(ctx, s.logger)
	}

	return &toolset, customDomainCtx, nil
}

func (s *Service) ServeAuthenticated(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var err error

	projectSlug := chi.URLParam(r, "project")
	if projectSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "a project slug must be provided")
	}

	toolsetSlug := chi.URLParam(r, "toolset")
	if toolsetSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "a toolset slug must be provided")
	}

	environmentSlug := chi.URLParam(r, "environment")
	if environmentSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an environment slug must be provided")
	}

	sc := security.APIKeyScheme{
		Name:           auth.KeySecurityScheme,
		Scopes:         []string{"consumer"},
		RequiredScopes: []string{},
	}
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	ctx, err = s.auth.Authorize(ctx, token, &sc)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}

	// Authorize with project
	sc = security.APIKeyScheme{
		Name:           auth.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err = s.auth.Authorize(ctx, projectSlug, &sc)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}

	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	// authorization check
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	var batch batchedRawRequest
	err = json.NewDecoder(r.Body).Decode(&batch)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").Log(ctx, s.logger)
	}

	if len(batch) == 0 {
		return respondWithNoContent(true, w)
	}

	mcpInputs := &mcpInputs{
		projectID:       *authCtx.ProjectID,
		toolset:         toolsetSlug,
		environment:     environmentSlug,
		mcpEnvVariables: parseMcpEnvVariables(r),
		authenticated:   true,
	}

	body, err := s.handleBatch(ctx, mcpInputs, batch)
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		return NewErrorFromCause(batch[0].ID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		s.logger.ErrorContext(ctx, "failed to write response body", slog.String("error", writeErr.Error()))
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}
	return nil
}

func (s *Service) handleBatch(ctx context.Context, payload *mcpInputs, batch batchedRawRequest) (json.RawMessage, error) {
	results := make([]json.RawMessage, 0, len(batch))
	for _, req := range batch {
		result, err := s.handleRequest(ctx, payload, req)
		switch {
		case result == nil && err == nil:
			return nil, nil
		case err != nil:
			bs, merr := json.Marshal(NewErrorFromCause(req.ID, err))
			if merr != nil {
				return nil, oops.E(oops.CodeUnexpected, merr, "failed to serialize error response").Log(ctx, s.logger)
			}

			result = bs
		}

		results = append(results, result)
	}

	if len(results) == 1 {
		return results[0], nil
	} else {
		m, err := json.Marshal(results)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize results")
		}

		return m, nil
	}
}

// parseMcpEnvVariables: Map potential user provided mcp variables into inputs
// Only inputs that match up with a security or server env var in the proxy will be used in the proxy
func parseMcpEnvVariables(r *http.Request) map[string]string {
	ignoredHeaders := []string{
		"mcp-session-id",
	}
	envVars := map[string]string{}
	for k := range r.Header {
		keySanitized := strings.ToLower(k)
		if strings.HasPrefix(keySanitized, "mcp-") && !slices.Contains(ignoredHeaders, keySanitized) {
			envVars[strings.ReplaceAll(strings.TrimPrefix(keySanitized, "mcp-"), "-", "_")] = r.Header.Get(k)
		}

	}

	return envVars
}

func (s *Service) handleRequest(ctx context.Context, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	switch req.Method {
	case "ping":
		return handlePing(ctx, s.logger, req.ID)
	case "initialize":
		return handleInitialize(ctx, s.logger, req)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return handleToolsList(ctx, s.logger, s.db, payload, req, s.posthog)
	case "tools/call":
		return handleToolsCall(ctx, s.logger, s.db, s.enc, payload, req, s.toolProxy)
	case "prompts/list":
		return handlePromptsList(ctx, s.logger, s.db, payload, req)
	case "prompts/get":
		return handlePromptsGet(ctx, s.logger, s.db, payload, req)
	default:
		return nil, &rpcError{
			ID:      req.ID,
			Code:    methodNotFound,
			Message: fmt.Sprintf("%s: %s", req.Method, methodNotFound.UserMessage()),
			Data:    nil,
		}
	}
}
