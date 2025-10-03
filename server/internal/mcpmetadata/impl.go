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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	repo        *repo.Queries
	toolsetRepo *toolsets_repo.Queries
	orgsRepo    *organizations_repo.Queries
	sessions    *sessions.Manager
	auth        *auth.Auth
	serverURL   *url.URL
}

//go:embed config_snippet.json.tmpl
var configSnippetTmplData string

//go:embed hosted_page.html.tmpl
var hostedPageTmplData string

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
	LogoAssetURL        string
	DocsURL             string
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("mcp_install_page"))

	return &Service{
		tracer:      otel.Tracer("github.com/speakeasy-api/gram/server/internal/mcpinstallpage"),
		logger:      logger,
		db:          db,
		repo:        repo.New(db),
		toolsetRepo: toolsets_repo.New(db),
		orgsRepo:    organizations_repo.New(db),
		sessions:    sessions,
		auth:        auth.New(logger, db, sessions),
		serverURL:   serverURL,
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

	// Attach hosted page routes
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}/install", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.ServeHostedPage).ServeHTTP(w, r)
	})
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
			logoAssetURL = fmt.Sprintf("/rpc/assets.serveImage?id=%s", metadataRecord.LogoID.UUID.String())
		}
		if docs := conv.FromPGText[string](metadataRecord.ExternalDocumentationUrl); docs != nil {
			docsURL = strings.TrimSpace(*docs)
		}
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
		MCPName:        toolset.Name,
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
		LogoAssetURL:        logoAssetURL,
		DocsURL:             docsURL,
	}

	hostedPageTmpl, err := template.New("hosted_page").Funcs(template.FuncMap{
		"diff": func(a, b int) int { return a - b },
		"indent": func(spaces int, text string) string {
			if spaces <= 0 || text == "" {
				return text
			}
			indent := strings.Repeat(" ", spaces)
			lines := strings.Split(text, "\n")
			for i := 1; i < len(lines); i++ {
				if i == len(lines)-1 && lines[i] == "" {
					continue
				}
				lines[i] = indent + lines[i]
			}
			return strings.Join(lines, "\n")
		},
	}).Parse(hostedPageTmplData)
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

func (s *Service) loadToolsetFromMcpSlug(ctx context.Context, mcpSlug string) (*toolsets_repo.Toolset, *gateway.DomainContext, error) {
	var toolset toolsets_repo.Toolset
	var toolsetErr error
	var customDomainCtx *gateway.DomainContext
	if domainCtx := gateway.DomainFromContext(ctx); domainCtx != nil {
		toolset, toolsetErr = s.toolsetRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true},
		})
		customDomainCtx = domainCtx
	} else {
		toolset, toolsetErr = s.toolsetRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
	}

	if toolsetErr != nil {
		return nil, nil, oops.E(oops.CodeNotFound, toolsetErr, "mcp server not found").Log(ctx, s.logger)
	}

	return &toolset, customDomainCtx, nil
}
