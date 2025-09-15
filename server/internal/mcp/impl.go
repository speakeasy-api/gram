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
	"os"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	auth_repo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	logger            *slog.Logger
	tracer            trace.Tracer
	metrics           *metrics
	db                *pgxpool.Pool
	authRepo          *auth_repo.Queries
	toolsetsRepo      *toolsets_repo.Queries
	orgsRepo          *organizations_repo.Queries
	auth              *auth.Auth
	env               gateway.EnvironmentLoader
	serverURL         *url.URL
	posthog           *posthog.Posthog // posthog metrics will no-op if the dependency is not provided
	toolProxy         *gateway.ToolProxy
	oauthService      *oauth.Service
	oauthRepo         *oauth_repo.Queries
	billingTracker    billing.Tracker
	billingRepository billing.Repository
}

type oauthTokenInputs struct {
	securityKeys []string // can be empty if a single token applies to the whole server
	Token        string
}

type mcpInputs struct {
	projectID        uuid.UUID
	toolset          string
	environment      string
	mcpEnvVariables  map[string]string
	oauthTokenInputs []oauthTokenInputs
	authenticated    bool
	sessionID        string
}

//go:embed config_snippet.json.tmpl
var configSnippetTmplData string

//go:embed hosted_page.html.tmpl
var hostedPageTmplData string

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	env gateway.EnvironmentLoader,
	posthog *posthog.Posthog,
	serverURL *url.URL,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	oauthService *oauth.Service,
	billingTracker billing.Tracker,
	billingRepository billing.Repository,
) *Service {
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcp")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcp")
	logger = logger.With(attr.SlogComponent("mcp"))

	return &Service{
		logger:       logger,
		tracer:       tracer,
		metrics:      newMetrics(meter, logger),
		db:           db,
		authRepo:     auth_repo.New(db),
		toolsetsRepo: toolsets_repo.New(db),
		orgsRepo:     organizations_repo.New(db),
		auth:         auth.New(logger, db, sessions),
		env:          env,
		serverURL:    serverURL,
		posthog:      posthog,
		toolProxy: gateway.NewToolProxy(
			logger,
			tracerProvider,
			meterProvider,
			gateway.ToolCallSourceMCP,
			cacheImpl,
			guardianPolicy,
		),
		oauthService:      oauthService,
		oauthRepo:         oauth_repo.New(db),
		billingTracker:    billingTracker,
		billingRepository: billingRepository,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	o11y.AttachHandler(mux, "POST", "/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServePublic).ServeHTTP(w, r)
	})
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
		// This is page is being laoded in the browser request
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
			service.logger.ErrorContext(r.Context(), "failed to marshal MCP 405 response", attr.SlogError(err))
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, writeErr := w.Write(body)
		if writeErr != nil {
			service.logger.ErrorContext(r.Context(), "failed to write response body", attr.SlogError(writeErr))
		}
	})
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}/install", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServeHostedPage).ServeHTTP(w, r)
	})
	o11y.AttachHandler(mux, "POST", "/mcp/{project}/{toolset}/{environment}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServeAuthenticated).ServeHTTP(w, r)
	})

	// OAuth 2.1 Authorization Server Metadata
	o11y.AttachHandler(mux, "GET", "/.well-known/oauth-authorization-server/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.HandleWellKnownOAuthServerMetadata).ServeHTTP(w, r)
	})

	o11y.AttachHandler(mux, "GET", "/.well-known/oauth-protected-resource/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.HandleWellKnownOAuthProtectedResourceMetadata).ServeHTTP(w, r)
	})
}

// handleWellKnownMetadata handles OAuth 2.1 authorization server metadata discovery
func (s *Service) HandleWellKnownOAuthServerMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	var metadata map[string]interface{}
	switch {
	case toolset.OauthProxyServerID.Valid:
		metadata = map[string]interface{}{
			"issuer":                           baseURL + "/oauth/" + mcpSlug,
			"authorization_endpoint":           baseURL + "/oauth/" + mcpSlug + "/authorize",
			"token_endpoint":                   baseURL + "/oauth/" + mcpSlug + "/token",
			"registration_endpoint":            baseURL + "/oauth/" + mcpSlug + "/register",
			"response_types_supported":         []string{"code"},
			"grant_types_supported":            []string{"authorization_code"},
			"code_challenge_methods_supported": []string{"plain", "S256"},
		}
	case toolset.ExternalOauthServerID.Valid:
		// Fetch metadata from external_oauth_server_metadata table
		externalOAuthServer, err := s.oauthRepo.GetExternalOAuthServerMetadata(ctx, oauth_repo.GetExternalOAuthServerMetadataParams{
			ProjectID: toolset.ProjectID,
			ID:        toolset.ExternalOauthServerID.UUID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load OAuth server metadata").Log(ctx, s.logger)
		}

		// Parse the stored JSON metadata
		if err := json.Unmarshal(externalOAuthServer.Metadata, &metadata); err != nil {
			return oops.E(oops.CodeUnexpected, err, "invalid OAuth server metadata").Log(ctx, s.logger)
		}
	default:
		return oops.E(oops.CodeNotFound, nil, "").Log(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	body, err := json.Marshal(metadata)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth server metadata").Log(ctx, s.logger)
	}

	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) HandleWellKnownOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	switch {
	case toolset.OauthProxyServerID.Valid, toolset.ExternalOauthServerID.Valid:
		// Continue processing
	default:
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found for this MCP server").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	metadata := map[string]any{
		"issuer":                baseURL + "/mcp/" + mcpSlug,
		"authorization_servers": []string{baseURL + "/mcp/" + mcpSlug},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	body, err := json.Marshal(metadata)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth protected resource metadata").Log(ctx, s.logger)
	}

	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body").Log(ctx, s.logger)
	}

	return nil
}

type jsonSnippetData struct {
	MCPName        string
	MCPSlug        string
	MCPDescription string
	Headers        []string
	EnvHeaders     []string
	MCPURL         string
	ToolNames      []string
}

type hostedPageData struct {
	jsonSnippetData
	MCPConfig           string
	MCPConfigURIEncoded string
	OrganizationName    string
	SiteURL             string
}

func (s *Service) ServeHostedPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	if !toolset.McpIsPublic {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	// Load organization information
	organization, err := s.orgsRepo.GetOrganizationMetadata(ctx, toolset.OrganizationID)
	var organizationName string
	if err != nil {
		s.logger.WarnContext(ctx, "could not load organization information", attr.SlogOrganizationID(toolset.OrganizationID), attr.SlogError(err))
		organizationName = "Unknown Organization"
	} else {
		organizationName = organization.Name
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to describe toolset").Log(ctx, s.logger)
	}

	envHeaders := []string{}

	// Collect environment variables from security variables
	for _, secVar := range toolsetDetails.SecurityVariables {
		for _, envVar := range secVar.EnvVariables {
			if !strings.Contains(strings.ToLower(envVar), "token_url") {
				envHeaders = append(envHeaders, fmt.Sprintf("MCP-%s", strings.ReplaceAll(envVar, "_", "-")))
			}
		}
	}

	toolNames := []string{}

	for _, toolDesc := range toolsetDetails.HTTPTools {
		toolNames = append(toolNames, toolDesc.Name)
	}

	for _, promptTpl := range toolsetDetails.PromptTemplates {
		if promptTpl.Kind == "higher_order_tool" {
			toolNames = append(toolNames, string(promptTpl.Name))
		}
	}

	baseURL := s.serverURL.String() + "/mcp"
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain+"/mcp")
	}
	MCPURL, err := url.JoinPath(baseURL, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "malformed mcp url").Log(ctx, s.logger)
	}

	// Create env-safe versions of headers (replace dashes with underscores)
	envHeadersEnvSafe := make([]string, len(envHeaders))
	for i, header := range envHeaders {
		envHeadersEnvSafe[i] = strings.ReplaceAll(header, "-", "_")
	}

	configSnippetData := jsonSnippetData{
		MCPName:        cases.Title(language.English).String(toolset.Name),
		MCPSlug:        toolset.Slug,
		MCPDescription: toolset.Description.String,
		MCPURL:         MCPURL,
		Headers:        envHeaders,
		EnvHeaders:     envHeadersEnvSafe,
		ToolNames:      toolNames,
	}

	configSnippetTmpl, err := template.New("config_snippet").Parse(configSnippetTmplData)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to parse config snippet template").Log(ctx, s.logger)
	}

	var configSnippet bytes.Buffer
	if err := configSnippetTmpl.Execute(&configSnippet, configSnippetData); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to execute config snippet template").Log(ctx, s.logger)
	}

	data := hostedPageData{
		jsonSnippetData:     configSnippetData,
		MCPConfig:           configSnippet.String(),
		MCPConfigURIEncoded: url.QueryEscape(base64.StdEncoding.EncodeToString(configSnippet.Bytes())),
		OrganizationName:    organizationName,
		SiteURL:             os.Getenv("GRAM_SITE_URL"),
	}

	hostedPageTmpl, err := template.New("hosted_page").Parse(hostedPageTmplData)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to parse hosted page template").Log(ctx, s.logger)
	}

	buf := &bytes.Buffer{}
	if err := hostedPageTmpl.Execute(buf, data); err != nil {
		s.logger.ErrorContext(ctx, "failed to execute hosted page template", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "failed to execute hosted page template")
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(buf.Bytes())
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to write response body", attr.SlogError(err))
	}

	return nil
}

func (s *Service) ServePublic(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	var tokenInputs []oauthTokenInputs

	switch {
	case toolset.McpIsPublic && toolset.ExternalOauthServerID.Valid:
		if token == "" {
			s.logger.WarnContext(ctx, "No authorization token provided")
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%s`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug))
			return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
		}

		tokenInputs = append(tokenInputs, oauthTokenInputs{
			securityKeys: []string{},
			Token:        token,
		})
	case toolset.McpIsPublic && toolset.OauthProxyServerID.Valid:
		token, err := s.oauthService.ValidateAccessToken(ctx, toolset.ID, token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%s`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug))
			return oops.E(oops.CodeUnauthorized, err, "invalid or expired access token").Log(ctx, s.logger)
		}
		s.logger.InfoContext(ctx, "OAuth token validated successfully", attr.SlogToolsetID(toolset.ID.String()))

		for _, externalSecret := range token.ExternalSecrets {
			tokenInputs = append(tokenInputs, oauthTokenInputs{
				securityKeys: externalSecret.SecurityKeys,
				Token:        externalSecret.Token,
			})
		}
	default:
		if token != "" {
			// see if we are authenticated with our own key
			sc := security.APIKeyScheme{
				Name:           auth.KeySecurityScheme,
				RequiredScopes: []string{"consumer"},
				Scopes:         []string{},
			}
			ctx, err = s.auth.Authorize(ctx, token, &sc)
			if err != nil {
				return oops.E(oops.CodeUnauthorized, err, "failed to authorize with API key").Log(ctx, s.logger)
			}
		}
	}

	var selectedEnvironment string
	var authenticated bool
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID != "" {
		projects, err := s.authRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return oops.E(oops.CodeForbidden, nil, "no projects found").Log(ctx, s.logger)
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "error checking project access").Log(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
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
		return oops.C(oops.CodeNotFound)
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

	sessionID := parseMcpSessionID(r.Header)
	w.Header().Set("Mcp-Session-Id", sessionID)

	mcpInputs := &mcpInputs{
		projectID:        toolset.ProjectID,
		toolset:          toolset.Slug,
		environment:      selectedEnvironment,
		mcpEnvVariables:  parseMcpEnvVariables(r),
		authenticated:    authenticated,
		oauthTokenInputs: tokenInputs,
		sessionID:        sessionID,
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
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}

	return nil
}

func (s *Service) loadToolsetFromMcpSlug(ctx context.Context, mcpSlug string) (*toolsets_repo.Toolset, *gateway.DomainContext, error) {
	var toolset toolsets_repo.Toolset
	var toolsetErr error
	var customDomainCtx *gateway.DomainContext
	if domainCtx := gateway.DomainFromContext(ctx); domainCtx != nil {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true},
		})
		customDomainCtx = domainCtx
	} else {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug)) //
	}

	if toolsetErr != nil {
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

	sessionID := parseMcpSessionID(r.Header)
	w.Header().Set("Mcp-Session-Id", sessionID)

	mcpInputs := &mcpInputs{
		projectID:        *authCtx.ProjectID,
		toolset:          toolsetSlug,
		environment:      environmentSlug,
		mcpEnvVariables:  parseMcpEnvVariables(r),
		authenticated:    true,
		oauthTokenInputs: []oauthTokenInputs{},
		sessionID:        sessionID,
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
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body").Log(ctx, s.logger)
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
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize results").Log(ctx, s.logger)
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
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		start := time.Now()
		defer func() {
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, time.Since(start))
		}()
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, s.logger, req.ID)
	case "initialize":
		return handleInitialize(ctx, s.logger, req, payload, s.posthog)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return handleToolsList(ctx, s.logger, s.db, payload, req, s.posthog)
	case "tools/call":
		return handleToolsCall(ctx, s.logger, s.metrics, s.db, s.env, payload, req, s.toolProxy, s.billingTracker, s.billingRepository)
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

func parseMcpSessionID(headers http.Header) string {
	session := headers.Get("Mcp-Session-Id")
	if session == "" {
		session = uuid.New().String()
	}
	return session
}
