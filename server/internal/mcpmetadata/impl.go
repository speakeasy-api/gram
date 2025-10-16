package mcpmetadata

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_metadata/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/templatefuncs"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	toolsetRepo  *toolsets_repo.Queries
	orgsRepo     *organizations_repo.Queries
	domainsRepo  *customdomains_repo.Queries
	auth         *auth.Auth
	serverURL    *url.URL
	toolsetCache cache.TypedCacheObject[mv.ToolsetTools]
}

//go:embed config_snippet.json.tmpl
var configSnippetTmplData string

//go:embed hosted_page.html.tmpl
var hostedPageTmplData string

type securityMode string

const (
	securityModePublic securityMode = "public"
	securityModeGram   securityMode = "gram"
	securityModeOAuth  securityMode = "oauth"
)

type securityInput struct {
	SystemName  string
	DisplayName string
	Sensitive   bool
}

type jsonSnippetData struct {
	MCPName        string
	MCPSlug        string
	MCPDescription string
	SecurityInputs []securityInput
	MCPURL         string
	ToolNames      []string
}

type hostedPageData struct {
	jsonSnippetData
	MCPConfig           string
	MCPConfigURIEncoded string
	OrganizationName    string
	SiteURL             string
	LogoAssetURL        string
	DocsURL             string
	IsPublic            bool
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, serverURL *url.URL, cacheAdapter cache.Cache) *Service {
	logger = logger.With(attr.SlogComponent("mcp_install_page"))

	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/server/internal/mcpinstallpage"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		toolsetRepo:  toolsets_repo.New(db),
		orgsRepo:     organizations_repo.New(db),
		domainsRepo:  customdomains_repo.New(db),
		auth:         auth.New(logger, db, sessions),
		serverURL:    serverURL,
		toolsetCache: cache.NewTypedObjectCache[mv.ToolsetTools](logger.With(attr.SlogCacheNamespace("toolset")), cacheAdapter, cache.SuffixNone),
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
}

func (s *Service) GetMcpMetadata(ctx context.Context, payload *gen.GetMcpMetadataPayload) (*gen.GetMcpMetadataResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeForbidden)
	}

	toolset, err := s.toolsetRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, err, "toolset not found").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch toolset").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	}

	record, err := s.repo.GetMetadataForToolset(ctx, toolset.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "no MCP install page metadata for this toolset").Log(ctx, s.logger, attr.SlogToolsetID(toolset.ID.String()))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch MCP install page metadata").Log(ctx, s.logger)
	}

	return &gen.GetMcpMetadataResult{
		Metadata: toMcpMetadata(record),
	}, nil
}

func (s *Service) SetMcpMetadata(ctx context.Context, payload *gen.SetMcpMetadataPayload) (*types.McpMetadata, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeForbidden)
	}

	toolset, err := s.toolsetRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, err, "toolset not found").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch toolset").Log(ctx, s.logger, slog.String("toolset_slug", string(payload.ToolsetSlug)))
	}

	var logoID uuid.NullUUID
	if payload.LogoAssetID != nil {
		parsedLogoID, err := uuid.Parse(*payload.LogoAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset ID").Log(ctx, s.logger)
		}
		logoID = uuid.NullUUID{UUID: parsedLogoID, Valid: true}
	}

	var externalDocURL pgtype.Text
	if payload.ExternalDocumentationURL != nil {
		externalDocURL = conv.ToPGText(*payload.ExternalDocumentationURL)
	}

	result, err := s.repo.UpsertMetadata(ctx, repo.UpsertMetadataParams{
		ToolsetID:                toolset.ID,
		ProjectID:                *authCtx.ProjectID,
		ExternalDocumentationUrl: externalDocURL,
		LogoID:                   logoID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert MCP install page metadata").Log(ctx, s.logger)
	}

	return toMcpMetadata(result), nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func toMcpMetadata(record repo.McpMetadatum) *types.McpMetadata {
	metadata := &types.McpMetadata{
		ID:                       record.ID.String(),
		ToolsetID:                record.ToolsetID.String(),
		CreatedAt:                record.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                record.UpdatedAt.Time.Format(time.RFC3339),
		ExternalDocumentationURL: conv.FromPGText[string](record.ExternalDocumentationUrl),
		LogoAssetID:              conv.FromNullableUUID(record.LogoID),
	}
	return metadata
}

func (s *Service) ServeInstallPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	sessionToken, _ := contextvalues.GetSessionTokenFromContext(ctx)

	ctx, _ = s.auth.Authorize(ctx, sessionToken, &security.APIKeyScheme{
		Name:           auth.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	})

	// We get the authCtx now, because we need session information in order to look up private servers
	// but we don't check that auth is ok unless we encounter a private toolset on lookup
	authCtx, authOk := contextvalues.GetAuthContext(ctx)

	toolset, err := s.loadToolsetFromContextAndSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	// Load organization information
	organization, err := s.orgsRepo.GetOrganizationMetadata(ctx, toolset.OrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mcp server not found").Log(ctx, s.logger)
	}

	if !toolset.McpIsPublic {
		// If no auth context, redirect to login page
		if authCtx == nil {
			if s.serverURL != nil {
				loginURL := s.serverURL.String() + "/login"
				http.Redirect(w, r, loginURL, http.StatusFound)
				return nil
			}
			// Fallback if serverURL is nil
			return oops.E(oops.CodeNotFound, nil, "mcp server not found").Log(ctx, s.logger)
		}

		// Ought one to check if the user has access to the organization rather than just if the org is active?
		if !authOk || authCtx.ActiveOrganizationID != toolset.OrganizationID {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
		}
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to describe toolset").Log(ctx, s.logger)
	}

	var (
		logoAssetURL string
		docsURL      string
	)
	metadataRecord, metadataErr := s.repo.GetMetadataForToolset(ctx, toolset.ID)
	if metadataErr != nil {
		if !errors.Is(metadataErr, pgx.ErrNoRows) {
			s.logger.WarnContext(ctx, "failed to load MCP install page metadata", attr.SlogToolsetID(toolset.ID.String()), attr.SlogError(metadataErr))
		}
	} else {
		if metadataRecord.LogoID.Valid {
			logoURL := *s.serverURL
			logoURL.Path = "/rpc/assets.serveImage"
			q := logoURL.Query()
			q.Set("id", metadataRecord.LogoID.UUID.String())
			logoURL.RawQuery = q.Encode()
			logoAssetURL = logoURL.String()
		}
		if docs := conv.FromPGText[string](metadataRecord.ExternalDocumentationUrl); docs != nil {
			docsURL = strings.TrimSpace(*docs)
		}
	}

	securityMode := s.resolveSecurityMode(toolset)
	securityInputs := s.collectEnvironmentVariables(securityMode, toolsetDetails)

	toolNames := []string{}

	for _, toolDesc := range toolsetDetails.Tools {
		baseTool := conv.ToBaseTool(toolDesc)
		toolNames = append(toolNames, baseTool.Name)
	}

	MCPURL, err := resolveMCPURLFromContext(ctx, s.serverURL.String(), mcpSlug, toolset.McpIsPublic)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolved bad url").Log(ctx, s.logger)
	}

	configSnippetData := jsonSnippetData{
		MCPName:        toolset.Name,
		MCPSlug:        toolset.Slug,
		MCPDescription: toolset.Description.String,
		MCPURL:         MCPURL,
		SecurityInputs: securityInputs,
		ToolNames:      toolNames,
	}

	configSnippetTmpl, err := template.New("config_snippet").Funcs(templatefuncs.FuncMap()).Parse(configSnippetTmplData)
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
		OrganizationName:    organization.Name,
		SiteURL:             os.Getenv("GRAM_SITE_URL"),
		LogoAssetURL:        logoAssetURL,
		DocsURL:             docsURL,
		IsPublic:            toolset.McpIsPublic,
	}

	hostedPageTmpl, err := template.New("hosted_page").Funcs(templatefuncs.FuncMap()).Parse(hostedPageTmplData)
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
	_, writeErr := w.Write(buf.Bytes())
	if writeErr != nil {
		s.logger.ErrorContext(ctx, "failed to write response body", attr.SlogError(writeErr))
	}

	return nil
}

func resolveMCPURLFromContext(ctx context.Context, serverUrl string, mcpSlug string, serverIsPublic bool) (string, error) {
	customDomainCtx := customdomains.FromContext(ctx)
	baseURL := serverUrl + "/mcp"
	if !serverIsPublic && customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain+"/mcp")
	}
	MCPURL, err := url.JoinPath(baseURL, mcpSlug)
	if err != nil {
		return "", fmt.Errorf("failed to join URL path: %w", err)
	}
	return MCPURL, nil
}

func (s *Service) resolveDomainIDFromContext(ctx context.Context) *uuid.UUID {
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		return &domainCtx.DomainID
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)

	if authCtx == nil {
		return nil
	}

	domainRecord, err := s.domainsRepo.GetCustomDomainsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get custom domains by organization ID", attr.SlogError(err))
		return nil
	}
	return &domainRecord.ID
}

func (s *Service) loadToolsetFromContextAndSlug(ctx context.Context, mcpSlug string) (*toolsets_repo.Toolset, error) {
	var toolset toolsets_repo.Toolset
	var toolsetErr error
	domainID := s.resolveDomainIDFromContext(ctx)
	if domainID != nil {
		toolset, toolsetErr = s.toolsetRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: *domainID, Valid: true},
		})
	}

	// Fallback to just looking up by slug if no domain in context or if lookup by domain failed
	if domainID == nil || toolsetErr != nil {
		toolset, toolsetErr = s.toolsetRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
	}

	if toolsetErr != nil {
		return nil, oops.E(oops.CodeNotFound, toolsetErr, "mcp server not found").Log(ctx, s.logger)
	}

	return &toolset, nil
}

// resolveSecurityMode determines the security mode based on toolset configuration
// Prefers oauth > gram > public
func (s *Service) resolveSecurityMode(toolset *toolsets_repo.Toolset) securityMode {
	if toolset.OauthProxyServerID.Valid || toolset.ExternalOauthServerID.Valid {
		return securityModeOAuth
	}

	if toolset.McpIsPublic {
		return securityModePublic
	}

	return securityModeGram
}

// collectEnvironmentVariables returns security inputs based on the security mode
func (s *Service) collectEnvironmentVariables(mode securityMode, toolsetDetails *types.Toolset) []securityInput {
	switch mode {
	case securityModeGram:
		return []securityInput{
			{
				SystemName:  "gram_environment",
				DisplayName: "gram-environment",
				Sensitive:   false,
			},
			{
				SystemName:  "authorization",
				DisplayName: "gram-key",
				Sensitive:   true,
			},
		}

	case securityModePublic, securityModeOAuth:
		var inputs []securityInput
		isOAuthEnabled := mode == securityModeOAuth

		for _, secVar := range toolsetDetails.SecurityVariables {
			for _, envVar := range secVar.EnvVariables {
				envVarLower := strings.ToLower(envVar)
				if strings.HasSuffix(envVarLower, "token_url") {
					continue
				}
				// Skip access_token env vars if OAuth is enabled
				if isOAuthEnabled && strings.HasSuffix(envVarLower, "access_token") {
					continue
				}

				inputs = append(inputs, securityInput{
					SystemName:  fmt.Sprintf("MCP-%s", envVar),
					DisplayName: fmt.Sprintf("MCP-%s", strings.ReplaceAll(envVar, "_", "-")),
					Sensitive:   true,
				})
			}
		}

		for _, functionEnvVar := range toolsetDetails.FunctionEnvironmentVariables {
			inputs = append(inputs, securityInput{
				SystemName:  fmt.Sprintf("MCP-%s", functionEnvVar.Name),
				DisplayName: fmt.Sprintf("MCP-%s", strings.ReplaceAll(functionEnvVar.Name, "_", "-")),
				Sensitive:   true,
			})
		}

		return inputs

	default:
		return []securityInput{}
	}
}
