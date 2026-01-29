package mcpmetadata

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
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
	"github.com/speakeasy-api/gram/server/internal/constants"
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
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
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
	siteURL      *url.URL
	toolsetCache cache.TypedCacheObject[mv.ToolsetBaseContents]
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

type IDEInstallLinkConfig struct {
	// Required for vscode, cursor
	URL string `json:"url"`
	// Applicable for vscode, cursor
	Headers map[string]string `json:"headers"`
	// Required for vscode
	Name *string `json:"name,omitempty"`
	// Required for vscode ("http" only)
	Type *string `json:"type,omitempty"`
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
	MCPConfig         string
	CursorInstallLink template.URL
	VSCodeInstallLink template.URL
	OrganizationName  string
	SiteURL           string
	LogoAssetURL      string
	DocsURL           string
	Instructions      string
	IsPublic          bool
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, serverURL *url.URL, siteURL *url.URL, cacheAdapter cache.Cache) *Service {
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
		siteURL:      siteURL,
		toolsetCache: cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheAdapter, cache.SuffixNone),
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
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
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

	metadata, err := ToMCPMetadata(ctx, s.repo, record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to convert metadata").Log(ctx, s.logger)
	}

	return &gen.GetMcpMetadataResult{
		Metadata: metadata,
	}, nil
}

func (s *Service) SetMcpMetadata(ctx context.Context, payload *gen.SetMcpMetadataPayload) (*types.McpMetadata, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
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

	var instructions pgtype.Text
	if payload.Instructions != nil {
		instructions = conv.ToPGText(*payload.Instructions)
	}

	var defaultEnvironmentID uuid.NullUUID
	if payload.DefaultEnvironmentID != nil {
		parsedDefaultEnvironmentID, err := uuid.Parse(*payload.DefaultEnvironmentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid default environment ID (not a valid UUID)").Log(ctx, s.logger)
		}
		defaultEnvironmentID = uuid.NullUUID{UUID: parsedDefaultEnvironmentID, Valid: true}
	}

	result, err := s.repo.UpsertMetadata(ctx, repo.UpsertMetadataParams{
		ToolsetID:                toolset.ID,
		ProjectID:                *authCtx.ProjectID,
		ExternalDocumentationUrl: externalDocURL,
		LogoID:                   logoID,
		Instructions:             instructions,
		DefaultEnvironmentID:     defaultEnvironmentID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert MCP install page metadata").Log(ctx, s.logger)
	}

	// Update environment entries
	if payload.EnvironmentConfigs != nil {
		// Delete all existing entries
		if err := s.repo.DeleteAllEnvironmentConfigs(ctx, result.ID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to delete existing environment configs").Log(ctx, s.logger)
		}

		for _, config := range payload.EnvironmentConfigs {
			var headerDisplayName pgtype.Text
			if config.HeaderDisplayName != nil {
				headerDisplayName = conv.ToPGText(*config.HeaderDisplayName)
			}

			_, err := s.repo.UpsertEnvironmentConfig(ctx, repo.UpsertEnvironmentConfigParams{
				ProjectID:         *authCtx.ProjectID,
				McpMetadataID:     result.ID,
				VariableName:      config.VariableName,
				HeaderDisplayName: headerDisplayName,
				ProvidedBy:        config.ProvidedBy,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert environment config").Log(ctx, s.logger)
			}
		}
	}

	return ToMCPMetadata(ctx, s.repo, result)
}

func (s *Service) ExportMcpMetadata(ctx context.Context, payload *gen.ExportMcpMetadataPayload) (*types.McpExport, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	mcpSlug := conv.ToLower(payload.McpSlug)
	toolset, err := s.toolsetRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "MCP server not found").Log(ctx, s.logger, slog.String("mcp_slug", mcpSlug))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch MCP server").Log(ctx, s.logger, slog.String("mcp_slug", mcpSlug))
	}

	// Verify the toolset belongs to the user's project
	if toolset.ProjectID != *authCtx.ProjectID {
		return nil, oops.E(oops.CodeNotFound, nil, "MCP server not found")
	}

	if !toolset.McpEnabled {
		return nil, oops.E(oops.CodeNotFound, nil, "MCP server not found")
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to describe toolset").Log(ctx, s.logger)
	}

	// Load MCP metadata (logo, docs, instructions)
	var logoURL *string
	var docsURL *string
	var instructions *string
	headerDisplayNames := make(map[string]string)

	metadataRecord, metadataErr := s.repo.GetMetadataForToolset(ctx, toolset.ID)
	if metadataErr == nil {
		if metadataRecord.LogoID.Valid {
			logoURLValue := *s.serverURL
			logoURLValue.Path = "/rpc/assets.serveImage"
			q := logoURLValue.Query()
			q.Set("id", metadataRecord.LogoID.UUID.String())
			logoURLValue.RawQuery = q.Encode()
			logoURL = conv.Ptr(logoURLValue.String())
		}
		docsURL = conv.FromPGText[string](metadataRecord.ExternalDocumentationUrl)
		instructions = conv.FromPGText[string](metadataRecord.Instructions)
		if len(metadataRecord.HeaderDisplayNames) > 0 {
			if err := json.Unmarshal(metadataRecord.HeaderDisplayNames, &headerDisplayNames); err != nil {
				s.logger.ErrorContext(ctx, "failed to unmarshal header display names", attr.SlogError(err))
			}
		}
	} else if !errors.Is(metadataErr, pgx.ErrNoRows) {
		s.logger.WarnContext(ctx, "failed to load MCP metadata", attr.SlogToolsetID(toolset.ID.String()), attr.SlogError(metadataErr))
	}

	// Build MCP URL
	mcpURL, err := s.resolveMCPURLFromContext(ctx, toolset, s.serverURL.String())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve MCP URL").Log(ctx, s.logger)
	}

	// Collect security inputs
	securityMode := s.resolveSecurityMode(&toolset)
	securityInputs := s.collectEnvironmentVariables(securityMode, toolsetDetails, headerDisplayNames, map[string]bool{})

	// Build tools list
	exportTools := s.buildExportTools(ctx, toolsetDetails)

	// Build authentication info
	authRequired := len(securityInputs) > 0
	authHeaders := make([]*types.McpExportAuthHeader, 0, len(securityInputs))
	for _, input := range securityInputs {
		authHeaders = append(authHeaders, &types.McpExportAuthHeader{
			Name:        toolconfig.ToHTTPHeader(input.SystemName),
			DisplayName: input.DisplayName,
		})
	}

	return &types.McpExport{
		Name:             toolset.Name,
		Slug:             toolset.Slug,
		Description:      conv.FromPGText[string](toolset.Description),
		ServerURL:        mcpURL,
		DocumentationURL: docsURL,
		LogoURL:          logoURL,
		Instructions:     instructions,
		Tools:            exportTools,
		Authentication: &types.McpExportAuthentication{
			Required: authRequired,
			Headers:  authHeaders,
		},
	}, nil
}

func (s *Service) buildExportTools(ctx context.Context, toolsetDetails *types.Toolset) []*types.McpExportTool {
	tools := make([]*types.McpExportTool, 0, len(toolsetDetails.Tools))

	for _, toolDesc := range toolsetDetails.Tools {
		// Handle proxy tools (external MCP)
		if conv.IsProxyTool(toolDesc) {
			// Parse schema from JSON string to any
			var inputSchema any
			if toolDesc.ExternalMcpToolDefinition.Schema != "" {
				if err := json.Unmarshal([]byte(toolDesc.ExternalMcpToolDefinition.Schema), &inputSchema); err != nil {
					s.logger.WarnContext(ctx, "failed to unmarshal tool schema",
						attr.SlogError(err),
						attr.SlogToolName(toolDesc.ExternalMcpToolDefinition.Name))
				}
			}
			if inputSchema == nil {
				inputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
			}

			tools = append(tools, &types.McpExportTool{
				Name:        toolDesc.ExternalMcpToolDefinition.Name,
				Description: toolDesc.ExternalMcpToolDefinition.Description,
				InputSchema: inputSchema,
			})
			continue
		}

		baseTool, err := conv.ToBaseTool(toolDesc)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to convert tool to base tool", attr.SlogError(err))
			continue
		}

		// Parse schema from JSON string to any
		var inputSchema any
		if baseTool.Schema != "" {
			if err := json.Unmarshal([]byte(baseTool.Schema), &inputSchema); err != nil {
				s.logger.WarnContext(ctx, "failed to unmarshal tool schema",
					attr.SlogError(err),
					attr.SlogToolName(baseTool.Name))
			}
		}
		if inputSchema == nil {
			inputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
		}

		tools = append(tools, &types.McpExportTool{
			Name:        baseTool.Name,
			Description: baseTool.Description,
			InputSchema: inputSchema,
		})
	}

	return tools
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func ToMCPMetadata(ctx context.Context, queries *repo.Queries, record repo.McpMetadatum) (*types.McpMetadata, error) {
	// Parse header display names from JSONB (deprecated field)
	headerDisplayNames := make(map[string]string)
	if len(record.HeaderDisplayNames) > 0 {
		_ = json.Unmarshal(record.HeaderDisplayNames, &headerDisplayNames)
	}

	// Fetch environment entries from the new table
	envEntries, err := queries.ListEnvironmentConfigs(ctx, record.ID)
	if err != nil {
		return nil, fmt.Errorf("list environment entries: %w", err)
	}

	// Create a map of existing entries by variable name
	existingEntries := make(map[string]repo.McpEnvironmentConfig)
	for _, entry := range envEntries {
		existingEntries[entry.VariableName] = entry
	}

	// Merge: start with entries from the new table, then add any from deprecated column that don't exist
	environmentConfigs := make([]*types.McpEnvironmentConfig, 0, len(envEntries)+len(headerDisplayNames))

	// Add all entries from the new table
	for _, entry := range envEntries {
		// Use header_display_name from the new table if set, otherwise check deprecated column
		var headerDisplayName *string
		if displayName := conv.FromPGText[string](entry.HeaderDisplayName); displayName != nil {
			headerDisplayName = displayName
		} else if deprecatedName, ok := headerDisplayNames[entry.VariableName]; ok {
			headerDisplayName = &deprecatedName
		}

		apiEntry := &types.McpEnvironmentConfig{
			ID:                entry.ID.String(),
			VariableName:      entry.VariableName,
			HeaderDisplayName: headerDisplayName,
			ProvidedBy:        entry.ProvidedBy,
			CreatedAt:         entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:         entry.UpdatedAt.Time.Format(time.RFC3339),
		}

		environmentConfigs = append(environmentConfigs, apiEntry)
	}

	// Add entries that only exist in the deprecated column (for backwards compatibility)
	for varName, displayName := range headerDisplayNames {
		if _, exists := existingEntries[varName]; !exists {
			// Create a synthetic entry for backwards compatibility
			apiEntry := &types.McpEnvironmentConfig{
				ID:                uuid.Nil.String(), // No ID since it's not in the new table
				VariableName:      varName,
				HeaderDisplayName: &displayName,
				ProvidedBy:        "user", // Default to user-provided
				CreatedAt:         record.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:         record.UpdatedAt.Time.Format(time.RFC3339),
			}
			environmentConfigs = append(environmentConfigs, apiEntry)
		}
	}

	metadata := &types.McpMetadata{
		ID:                       record.ID.String(),
		ToolsetID:                record.ToolsetID.String(),
		CreatedAt:                record.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                record.UpdatedAt.Time.Format(time.RFC3339),
		ExternalDocumentationURL: conv.FromPGText[string](record.ExternalDocumentationUrl),
		LogoAssetID:              conv.FromNullableUUID(record.LogoID),
		Instructions:             conv.FromPGText[string](record.Instructions),
		DefaultEnvironmentID:     conv.FromNullableUUID(record.DefaultEnvironmentID),
		EnvironmentConfigs:       environmentConfigs,
	}
	return metadata, nil
}

func buildCursorInstallURL(toolsetName, mcpURL string, inputs []securityInput) (string, error) {
	config := IDEInstallLinkConfig{
		URL:     mcpURL,
		Headers: map[string]string{},
		Name:    nil,
		Type:    nil,
	}

	for _, input := range inputs {
		headerKey := toolconfig.ToHTTPHeader(input.SystemName)
		config.Headers[headerKey] = fmt.Sprintf("{{%s}}", input.DisplayName)
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	u := &url.URL{
		Scheme: "cursor",
		Host:   "anysphere.cursor-deeplink",
		Path:   "/mcp/install",
		RawQuery: url.Values{
			"name":   {toolsetName},
			"config": {base64.StdEncoding.EncodeToString(configBytes)},
		}.Encode(),
	}
	return u.String(), nil
}

func buildVSCodeInstallURL(toolsetName, mcpURL string, inputs []securityInput) (string, error) {
	config := IDEInstallLinkConfig{
		Name:    &toolsetName,
		Type:    conv.Ptr("http"),
		URL:     mcpURL,
		Headers: map[string]string{},
	}

	for _, input := range inputs {
		headerKey := toolconfig.ToHTTPHeader(input.SystemName)
		config.Headers[headerKey] = fmt.Sprintf("your-%s-value", input.DisplayName)
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	u := &url.URL{
		Scheme: "vscode",
		Opaque: "mcp/install?" + url.QueryEscape(string(configBytes)),
	}
	return u.String(), nil
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
		Name:           constants.SessionSecurityScheme,
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

	logoAssetURL := s.siteURL.String() + "/external/sticker-logo.png"

	var docsURL string
	var instructions string
	headerDisplayNames := make(map[string]string)
	variableProvidedBy := make(map[string]string)
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
		if inst := conv.FromPGText[string](metadataRecord.Instructions); inst != nil {
			instructions = strings.TrimSpace(*inst)
		}

		// Load header display names from environment entries table and deprecated column
		metadata, err := ToMCPMetadata(ctx, s.repo, metadataRecord)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to convert metadata to get header display names", attr.SlogToolsetID(toolset.ID.String()), attr.SlogError(err))
		} else {
			// Build maps of header display names and omitted variables
			for _, config := range metadata.EnvironmentConfigs {
				variableProvidedBy[config.VariableName] = config.ProvidedBy
				if config.HeaderDisplayName != nil {
					headerDisplayNames[config.VariableName] = *config.HeaderDisplayName
				}
			}
		}
	}

	securityMode := s.resolveSecurityMode(toolset)
	securityInputs := s.collectEnvironmentVariables(securityMode, toolsetDetails, headerDisplayNames, variableProvidedBy)

	toolNames := []string{}

	for _, toolDesc := range toolsetDetails.Tools {
		// Handle proxy tools (external MCP) separately - they show as a single proxy entry
		if conv.IsProxyTool(toolDesc) {
			toolNames = append(toolNames, toolDesc.ExternalMcpToolDefinition.Name)
			continue
		}

		baseTool, err := conv.ToBaseTool(toolDesc)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to convert tool to base tool", attr.SlogError(err))
			continue
		}
		toolNames = append(toolNames, baseTool.Name)
	}

	MCPURL, err := s.resolveMCPURLFromContext(ctx, *toolset, s.serverURL.String())
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

	cursorURL, err := buildCursorInstallURL(toolset.Name, MCPURL, securityInputs)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to build cursor install URL").Log(ctx, s.logger)
	}

	vsCodeURL, err := buildVSCodeInstallURL(toolset.Name, MCPURL, securityInputs)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to build vscode install URL").Log(ctx, s.logger)
	}

	safeVsCodeURL, err := safeTemplateURL(vsCodeURL, "vscode")
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to sanitize vscode install URL").Log(ctx, s.logger)
	}

	safeCursorURL, err := safeTemplateURL(cursorURL, "cursor")
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to sanitize cursor install URL").Log(ctx, s.logger)
	}

	data := hostedPageData{
		jsonSnippetData:   configSnippetData,
		MCPConfig:         configSnippet.String(),
		CursorInstallLink: safeCursorURL,
		VSCodeInstallLink: safeVsCodeURL,
		OrganizationName:  organization.Name,
		SiteURL:           s.siteURL.String(),
		LogoAssetURL:      logoAssetURL,
		DocsURL:           docsURL,
		Instructions:      instructions,
		IsPublic:          toolset.McpIsPublic,
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

// Ensure that the provided rawURL uses the allowedScheme. allowedScheme may
// not be a dangerous scheme (eg: "javascript"). The returned URL is also
// HTML-escaped.
func safeTemplateURL(rawURL string, allowedScheme string) (template.URL, error) {
	dangerousURLSchemes := []string{"javascript", "data", "vbscript", "file", "about", "blob"}

	for _, dangerousScheme := range dangerousURLSchemes {
		if strings.HasPrefix(strings.ToLower(rawURL), dangerousScheme+":") {
			return template.URL(""), oops.E(
				oops.CodeBadRequest,
				nil,
				"%s scheme is not allowed",
				dangerousScheme,
			).Log(context.Background(), nil)
		}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return template.URL(""), oops.E(
			oops.CodeBadRequest,
			err,
			"invalid URL",
		).Log(context.Background(), nil)
	}

	if u.Scheme != allowedScheme {
		return template.URL(""), oops.E(
			oops.CodeBadRequest,
			nil,
			"invalid URL scheme: %s",
			u.Scheme,
		).Log(context.Background(), nil)
	}

	return template.URL(u.String()), nil // #nosec G203 // This has been checked and escaped
}

func (s *Service) resolveMCPURLFromContext(ctx context.Context, toolset toolsets_repo.Toolset, serverUrl string) (string, error) {
	baseURL := serverUrl + "/mcp"

	if toolset.CustomDomainID.Valid {
		customDomain, err := s.domainsRepo.GetCustomDomainByID(ctx, toolset.CustomDomainID.UUID)
		if err != nil {
			return "", fmt.Errorf("failed to get custom domain: %w", err)
		}

		baseURL = fmt.Sprintf("https://%s/mcp", customDomain.Domain)
	}

	// We will always have a valid McpSlug here, because we would not make it to this install view without it
	// MCP Slug is also always set on toolset creation
	MCPURL, err := url.JoinPath(baseURL, toolset.McpSlug.String)
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

	domainRecord, err := s.domainsRepo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
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
	if toolset.McpIsPublic {
		if toolset.OauthProxyServerID.Valid || toolset.ExternalOauthServerID.Valid {
			return securityModeOAuth
		}

		return securityModePublic
	}

	return securityModeGram
}

// collectEnvironmentVariables returns security inputs based on the security mode.
// headerDisplayNames maps env var names to their custom display names (from MCP metadata).
func (s *Service) collectEnvironmentVariables(mode securityMode, toolsetDetails *types.Toolset, headerDisplayNames map[string]string, variableProvidedBy map[string]string) []securityInput {
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
		seen := make(map[string]bool)

		isExplicitlyNotUserProvided := func(variableName string) bool {
			// This exists check ensure backwards compatibility for when there are no environment entries for a variable
			providedBy, exists := variableProvidedBy[variableName]
			return exists && providedBy != "user"
		}

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

				if isExplicitlyNotUserProvided(envVar) {
					continue
				}

				if !seen[envVar] {
					seen[envVar] = true

					// Priority for display name:
					// 1. Per-env-var display name from headerDisplayNames (keyed by env var name)
					// 2. Derive from env var name
					displayName := fmt.Sprintf("MCP-%s", strings.ReplaceAll(envVar, "_", "-"))
					if customName, ok := headerDisplayNames[envVar]; ok && customName != "" {
						displayName = customName
					}

					inputs = append(inputs, securityInput{
						SystemName:  fmt.Sprintf("MCP-%s", envVar),
						DisplayName: displayName,
						Sensitive:   true,
					})
				}
			}
		}

		for _, functionEnvVar := range toolsetDetails.FunctionEnvironmentVariables {
			if !seen[functionEnvVar.Name] {
				seen[functionEnvVar.Name] = true
				if isOAuthEnabled && functionEnvVar.AuthInputType != nil && *functionEnvVar.AuthInputType == "oauth2" {
					continue
				}
				if isExplicitlyNotUserProvided(functionEnvVar.Name) {
					continue
				}

				// Check for custom display name for function env vars too
				displayName := fmt.Sprintf("MCP-%s", strings.ReplaceAll(functionEnvVar.Name, "_", "-"))
				if customName, ok := headerDisplayNames[functionEnvVar.Name]; ok && customName != "" {
					displayName = customName
				}

				inputs = append(inputs, securityInput{
					SystemName:  fmt.Sprintf("MCP-%s", functionEnvVar.Name),
					DisplayName: displayName,
					Sensitive:   true,
				})
			}
		}

		for _, headerDef := range toolsetDetails.ExternalMcpHeaderDefinitions {
			if !headerDef.Required {
				continue
			}
			if !seen[headerDef.Name] {
				seen[headerDef.Name] = true
				inputs = append(inputs, securityInput{
					SystemName:  fmt.Sprintf("MCP-%s", headerDef.Name),
					DisplayName: fmt.Sprintf("MCP-%s", strings.ReplaceAll(headerDef.Name, "_", "-")),
					Sensitive:   headerDef.Secret,
				})
			}
		}

		// Add in any variables that were explicitly set to "user provided"
		for variableName, providedBy := range variableProvidedBy {
			if providedBy != "user" {
				continue
			}
			if !seen[variableName] {
				seen[variableName] = true
				inputs = append(inputs, securityInput{
					SystemName:  fmt.Sprintf("MCP-%s", variableName),
					DisplayName: fmt.Sprintf("MCP-%s", strings.ReplaceAll(variableName, "_", "-")),
					Sensitive:   true,
				})
			}
		}

		return inputs

	default:
		return []securityInput{}
	}
}
