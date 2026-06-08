package mcpmetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_metadata/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/templatefuncs"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

//go:embed config_snippet.json.tmpl
var configSnippetTmplData string

//go:embed hosted_page.html.tmpl
var hostedPageTmplData string

//go:embed not_found_page.html.tmpl
var notFoundPageTmplData string

//go:embed hosted_page.js
var hostedPageScriptData []byte

var errToolsetNotFound = errors.New("toolset not found")

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

type toolInfo struct {
	Name            string
	Description     string
	Title           string
	ReadOnlyHint    bool
	DestructiveHint bool
	IdempotentHint  bool
	OpenWorldHint   bool
	// Tags are the tool's effective filter tags (see toolfilter.EffectiveToolTags).
	// Rendered onto each tool row so the install-page JS can filter the list by
	// the selected scope without re-deriving tags client-side.
	Tags []string
}

// scopeInfo is one selectable filter scope (a single tag) shown as a chip on the
// install page, along with the number of tools that carry that tag.
type scopeInfo struct {
	Tag       string
	ToolCount int
}

// scopeConnection holds the per-scope connection strings the install-page JS
// swaps in when a scope chip is selected. Every value is built server-side from
// the existing Go URL builders so the client performs no URL/encoding logic and
// cannot drift from the runtime ?tags= behavior. The map emitted to the page is
// keyed by tag, with the empty-string key holding the unfiltered defaults.
type scopeConnection struct {
	URL    string       `json:"url"`
	Config string       `json:"config"`
	Cursor template.URL `json:"cursor"`
	VSCode template.URL `json:"vscode"`
}

func applyAnnotations(info *toolInfo, a *types.ToolAnnotations) {
	if a == nil {
		return
	}
	if a.Title != nil {
		info.Title = *a.Title
	}
	if a.ReadOnlyHint != nil {
		info.ReadOnlyHint = *a.ReadOnlyHint
	}
	if a.DestructiveHint != nil {
		info.DestructiveHint = *a.DestructiveHint
	}
	if a.IdempotentHint != nil {
		info.IdempotentHint = *a.IdempotentHint
	}
	if a.OpenWorldHint != nil {
		info.OpenWorldHint = *a.OpenWorldHint
	}
}

type jsonSnippetData struct {
	MCPName        string
	MCPSlug        string
	MCPDescription string
	SecurityInputs []securityInput
	MCPURL         string
	Tools          []toolInfo
}

type hostedPageData struct {
	jsonSnippetData
	MCPConfig         string
	CursorInstallLink template.URL
	VSCodeInstallLink template.URL
	OrganizationName  string
	SiteURL           string
	ScriptURL         string
	LogoAssetURL      string
	DocsURL           string
	DocsText          string
	Instructions      string
	IsPublic          bool
	// IsOAuth is true when the server authenticates via OAuth (proxy, external,
	// or user-session issuer). Drives the OAuth-only install steps (e.g. the
	// Codex `codex mcp login` step).
	IsOAuth bool
	// FilteringEnabled gates the scope chip bar and related UI.
	FilteringEnabled bool
	// Scopes are the selectable filter tags rendered as chips.
	Scopes []scopeInfo
	// ScopeVariantsJSON is a JSON object keyed by tag (empty-string key = the
	// unfiltered default) mapping to that scope's connection strings. It is
	// emitted into a data-* attribute and read by the install-page JS;
	// html/template auto-escapes it in attribute context.
	ScopeVariantsJSON string
}

type Service struct {
	tracer         trace.Tracer
	logger         *slog.Logger
	db             *pgxpool.Pool
	repo           *repo.Queries
	toolsetRepo    *toolsets_repo.Queries
	mcpServersRepo *mcpservers_repo.Queries
	projectsRepo   *projects_repo.Queries
	orgsRepo       *organizations_repo.Queries
	domainsRepo    *customdomains_repo.Queries
	auth           *auth.Auth
	serverURL      *url.URL
	siteURL        *url.URL
	toolsetCache   cache.TypedCacheObject[mv.ToolsetBaseContents]
	audit          *audit.Logger

	// Hosted install page script (embedded and served with cache-busting hash)
	installPageScriptHash string
	installPageScriptData []byte
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	serverURL *url.URL,
	siteURL *url.URL,
	cacheAdapter cache.Cache,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("mcp_metadata"))

	// Calculate content hash for install page script (for cache busting)
	scriptHash := sha256.Sum256(hostedPageScriptData)
	scriptHashStr := hex.EncodeToString(scriptHash[:])[:8]

	return &Service{
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpmetadata"),
		logger:         logger,
		db:             db,
		repo:           repo.New(db),
		toolsetRepo:    toolsets_repo.New(db),
		mcpServersRepo: mcpservers_repo.New(db),
		projectsRepo:   projects_repo.New(db),
		orgsRepo:       organizations_repo.New(db),
		domainsRepo:    customdomains_repo.New(db),
		auth:           auth.New(logger, db, sessions, authzEngine),
		serverURL:      serverURL,
		siteURL:        siteURL,
		toolsetCache:   cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheAdapter, cache.SuffixNone),
		audit:          auditLogger,

		installPageScriptHash: scriptHashStr,
		installPageScriptData: hostedPageScriptData,
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) GetMcpMetadata(ctx context.Context, payload *gen.GetMcpMetadataPayload) (*gen.GetMcpMetadataResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogProjectSlug(conv.PtrValOr(authCtx.ProjectSlug, "")),
	)

	backend, err := resolveMetadataBackend(ctx, logger, s.toolsetRepo, s.mcpServersRepo, *authCtx.ProjectID, payload.ToolsetSlug, payload.McpServerID)
	if err != nil {
		return nil, err
	}

	var record repo.McpMetadatum
	switch {
	case backend.toolset != nil:
		record, err = s.repo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: backend.toolset.ID, Valid: true})
	default:
		record, err = s.repo.GetMetadataByMcpServerID(ctx, uuid.NullUUID{UUID: backend.mcpServer.ID, Valid: true})
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "no MCP install page metadata for this backend")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "fetch MCP install page metadata").Log(ctx, logger)
	}

	metadata, err := ToMCPMetadata(ctx, s.repo, record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "convert metadata").Log(ctx, logger)
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

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogProjectSlug(conv.PtrValOr(authCtx.ProjectSlug, "")),
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "access mcp server metadata").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	mcpr := repo.New(dbtx)
	tr := toolsets_repo.New(dbtx)
	msr := mcpservers_repo.New(dbtx)

	backend, err := resolveMetadataBackend(ctx, logger, tr, msr, *authCtx.ProjectID, payload.ToolsetSlug, payload.McpServerID)
	if err != nil {
		return nil, err
	}

	switch {
	case backend.toolset != nil:
		logger = logger.With(
			attr.SlogToolsetID(backend.toolset.ID.String()),
			attr.SlogToolsetSlug(backend.toolset.Slug),
		)
	default:
		logger = logger.With(
			attr.SlogMcpServerID(backend.mcpServer.ID.String()),
		)
	}

	var existing *types.McpMetadata
	var existingRow repo.McpMetadatum
	switch {
	case backend.toolset != nil:
		existingRow, err = mcpr.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: backend.toolset.ID, Valid: true})
	default:
		existingRow, err = mcpr.GetMetadataByMcpServerID(ctx, uuid.NullUUID{UUID: backend.mcpServer.ID, Valid: true})
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No existing metadata, proceed with creation
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "fetch existing MCP server metadata").Log(ctx, logger)
	default:
		existing, err = ToMCPMetadata(ctx, mcpr, existingRow)
		if err != nil {
			logger.ErrorContext(ctx, "convert existing MCP server metadata", attr.SlogError(err))
		}
	}

	var logoID uuid.NullUUID
	if payload.LogoAssetID != nil {
		parsedLogoID, err := uuid.Parse(*payload.LogoAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset ID").Log(ctx, logger)
		}
		logoID = uuid.NullUUID{UUID: parsedLogoID, Valid: true}
	}

	var externalDocURL pgtype.Text
	if payload.ExternalDocumentationURL != nil {
		externalDocURL = conv.ToPGText(*payload.ExternalDocumentationURL)
	}

	var externalDocText pgtype.Text
	if payload.ExternalDocumentationText != nil {
		externalDocText = conv.ToPGText(*payload.ExternalDocumentationText)
	}

	var instructions pgtype.Text
	if payload.Instructions != nil {
		instructions = conv.ToPGText(*payload.Instructions)
	}

	var defaultEnvironmentID uuid.NullUUID
	if payload.DefaultEnvironmentID != nil {
		if backend.mcpServer != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "default_environment_id is not yet supported for mcp_server-backed install pages").Log(ctx, logger)
		}

		parsedDefaultEnvironmentID, err := uuid.Parse(*payload.DefaultEnvironmentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid default environment ID (not a valid UUID)").Log(ctx, logger)
		}

		envr := environments_repo.New(dbtx)
		_, err = envr.GetEnvironmentByID(ctx, environments_repo.GetEnvironmentByIDParams{
			ID:        parsedDefaultEnvironmentID,
			ProjectID: *authCtx.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeBadRequest, err, "default environment not found in this project").Log(ctx, logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "validate default environment ID").Log(ctx, logger)
		}

		defaultEnvironmentID = uuid.NullUUID{UUID: parsedDefaultEnvironmentID, Valid: true}
	}

	var installationOverrideURL pgtype.Text
	if payload.InstallationOverrideURL != nil {
		installationOverrideURL = conv.ToPGText(*payload.InstallationOverrideURL)
	}

	var result repo.McpMetadatum
	switch {
	case backend.toolset != nil:
		result, err = mcpr.UpsertMetadata(ctx, repo.UpsertMetadataParams{
			ToolsetID:                 uuid.NullUUID{UUID: backend.toolset.ID, Valid: true},
			ProjectID:                 *authCtx.ProjectID,
			ExternalDocumentationUrl:  externalDocURL,
			ExternalDocumentationText: externalDocText,
			LogoID:                    logoID,
			Instructions:              instructions,
			DefaultEnvironmentID:      defaultEnvironmentID,
			InstallationOverrideUrl:   installationOverrideURL,
		})
	default:
		result, err = mcpr.UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
			McpServerID:               uuid.NullUUID{UUID: backend.mcpServer.ID, Valid: true},
			ProjectID:                 *authCtx.ProjectID,
			ExternalDocumentationUrl:  externalDocURL,
			ExternalDocumentationText: externalDocText,
			LogoID:                    logoID,
			Instructions:              instructions,
			DefaultEnvironmentID:      defaultEnvironmentID,
			InstallationOverrideUrl:   installationOverrideURL,
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert MCP server metadata").Log(ctx, logger)
	}

	// Update environment entries
	if payload.EnvironmentConfigs != nil {
		// Delete all existing entries
		if err := mcpr.DeleteAllEnvironmentConfigs(ctx, result.ID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "delete existing environment configs").Log(ctx, logger)
		}

		for _, config := range payload.EnvironmentConfigs {
			var headerDisplayName pgtype.Text
			if config.HeaderDisplayName != nil {
				headerDisplayName = conv.ToPGText(*config.HeaderDisplayName)
			}

			_, err := mcpr.UpsertEnvironmentConfig(ctx, repo.UpsertEnvironmentConfigParams{
				ProjectID:         *authCtx.ProjectID,
				McpMetadataID:     result.ID,
				VariableName:      config.VariableName,
				HeaderDisplayName: headerDisplayName,
				ProvidedBy:        config.ProvidedBy,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "upsert environment config").Log(ctx, logger)
			}
		}
	}

	metadata, err := ToMCPMetadata(ctx, mcpr, result)
	if err != nil {
		return nil, err
	}

	switch {
	case backend.toolset != nil:
		if err := s.audit.LogMCPMetadataUpdate(ctx, dbtx, audit.LogMCPMetadataUpdateEvent{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,

			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,

			ToolsetURN:  urn.NewToolset(backend.toolset.ID),
			ToolsetName: backend.toolset.Name,
			ToolsetSlug: backend.toolset.Slug,

			MCPMetadataSnapshotBefore: existing,
			MCPMetadataSnapshotAfter:  metadata,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log MCP server metadata update event").Log(ctx, logger)
		}
	default:
		if err := s.audit.LogMCPMetadataUpdateForMcpServer(ctx, dbtx, audit.LogMCPMetadataUpdateForMcpServerEvent{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,

			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,

			McpServerURN:  urn.NewMcpServer(backend.mcpServer.ID),
			McpServerName: conv.FromPGTextOrEmpty[string](backend.mcpServer.Name),
			McpServerSlug: conv.FromPGTextOrEmpty[string](backend.mcpServer.Slug),

			MCPMetadataSnapshotBefore: existing,
			MCPMetadataSnapshotAfter:  metadata,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log MCP server metadata update event").Log(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save MCP server metadata").Log(ctx, logger)
	}

	return metadata, nil
}

func (s *Service) ExportMcpMetadata(ctx context.Context, payload *gen.ExportMcpMetadataPayload) (*types.McpExport, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	mcpSlug := conv.ToLower(payload.McpSlug)
	toolset, err := s.toolsetRepo.GetToolsetByMcpSlugAndProject(ctx, toolsets_repo.GetToolsetByMcpSlugAndProjectParams{
		McpSlug:   conv.ToPGText(mcpSlug),
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "MCP server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch MCP server").Log(ctx, s.logger, attr.SlogToolsetMCPSlug(mcpSlug))
	}

	if !toolset.McpEnabled {
		return nil, oops.E(oops.CodeNotFound, nil, "MCP server not found")
	}

	// Resolve custom domain from the toolset's organization if not already set
	// Only use domains that are both activated and verified
	if !toolset.CustomDomainID.Valid {
		domainRecord, err := s.domainsRepo.GetCustomDomainByOrganization(ctx, toolset.OrganizationID)
		if err == nil && domainRecord.Activated && domainRecord.Verified {
			toolset.CustomDomainID = uuid.NullUUID{UUID: domainRecord.ID, Valid: true}
		}
		// Ignore errors - custom domain is optional
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to describe toolset").Log(ctx, s.logger)
	}

	// Load MCP metadata (logo, docs, instructions)
	var logoURL *string
	var docsURL *string
	var instructions *string
	headerDisplayNames := make(map[string]string)
	variableProvidedBy := make(map[string]string)

	metadataRecord, metadataErr := s.repo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true})
	if metadataErr == nil {
		if metadataRecord.LogoID.Valid {
			logoURLValue := *s.serverURL
			logoURLValue.Path = "/rpc/assets.serveImage"
			q := logoURLValue.Query()
			q.Set("id", metadataRecord.LogoID.UUID.String())
			logoURLValue.RawQuery = q.Encode()
			logoURL = new(logoURLValue.String())
		}
		docsURL = conv.FromPGText[string](metadataRecord.ExternalDocumentationUrl)
		instructions = conv.FromPGText[string](metadataRecord.Instructions)
		if len(metadataRecord.HeaderDisplayNames) > 0 {
			if err := json.Unmarshal(metadataRecord.HeaderDisplayNames, &headerDisplayNames); err != nil {
				s.logger.ErrorContext(ctx, "failed to unmarshal header display names", attr.SlogError(err))
			}
		}

		metadata, err := ToMCPMetadata(ctx, s.repo, metadataRecord)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to convert metadata").Log(ctx, s.logger)
		}
		for _, config := range metadata.EnvironmentConfigs {
			variableProvidedBy[config.VariableName] = config.ProvidedBy
			if config.HeaderDisplayName != nil {
				headerDisplayNames[config.VariableName] = *config.HeaderDisplayName
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
	securityMode := s.resolveSecurityMode(&toolset, nil)
	securityInputs := s.collectEnvironmentVariables(securityMode, toolsetDetails, headerDisplayNames, variableProvidedBy)

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

// resolvedMetadataBackend identifies the backing record for an install-page
// metadata RPC. Exactly one of toolset or mcpServer is non-nil. The toolset is
// scoped to the caller's project via toolsets.get; the mcp server is scoped via
// the (id, project_id) IDOR check on mcp_servers.get_by_id.
type resolvedMetadataBackend struct {
	toolset   *toolsets_repo.Toolset
	mcpServer *mcpservers_repo.McpServer
}

// resolveMetadataBackend validates the XOR shape on the payload, performs the
// project-scoped lookup that doubles as the IDOR check, and returns the
// resolved backend. The toolset/mcpServer queries are passed through so the
// caller can run either against the service pool or a transaction.
func resolveMetadataBackend(
	ctx context.Context,
	logger *slog.Logger,
	toolsetQueries *toolsets_repo.Queries,
	mcpServerQueries *mcpservers_repo.Queries,
	projectID uuid.UUID,
	toolsetSlug *types.Slug,
	mcpServerID *string,
) (*resolvedMetadataBackend, error) {
	toolsetProvided := toolsetSlug != nil && *toolsetSlug != ""
	mcpServerProvided := mcpServerID != nil && *mcpServerID != ""

	switch {
	case !toolsetProvided && !mcpServerProvided:
		return nil, oops.E(oops.CodeBadRequest, nil, "toolset_slug or mcp_server_id is required").Log(ctx, logger)
	case toolsetProvided && mcpServerProvided:
		return nil, oops.E(oops.CodeBadRequest, nil, "toolset_slug and mcp_server_id are mutually exclusive").Log(ctx, logger)
	case toolsetProvided:
		slug := conv.ToLower(string(*toolsetSlug))
		toolset, err := toolsetQueries.GetToolset(ctx, toolsets_repo.GetToolsetParams{
			Slug:      slug,
			ProjectID: projectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeBadRequest, err, "toolset not found").Log(ctx, logger, attr.SlogToolsetSlug(slug))
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "fetch toolset").Log(ctx, logger, attr.SlogToolsetSlug(slug))
		}
		return &resolvedMetadataBackend{toolset: &toolset, mcpServer: nil}, nil
	default:
		parsedID, err := uuid.Parse(*mcpServerID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id (not a valid UUID)").Log(ctx, logger)
		}
		server, err := mcpServerQueries.GetMCPServerByID(ctx, mcpservers_repo.GetMCPServerByIDParams{
			ID:        parsedID,
			ProjectID: projectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeBadRequest, err, "mcp server not found").Log(ctx, logger, attr.SlogMcpServerID(parsedID.String()))
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "fetch mcp server").Log(ctx, logger, attr.SlogMcpServerID(parsedID.String()))
		}
		return &resolvedMetadataBackend{toolset: nil, mcpServer: &server}, nil
	}
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
		ID:                        record.ID.String(),
		ToolsetID:                 conv.FromNullableUUID(record.ToolsetID),
		McpServerID:               conv.FromNullableUUID(record.McpServerID),
		CreatedAt:                 record.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                 record.UpdatedAt.Time.Format(time.RFC3339),
		ExternalDocumentationURL:  conv.FromPGText[string](record.ExternalDocumentationUrl),
		ExternalDocumentationText: conv.FromPGText[string](record.ExternalDocumentationText),
		LogoAssetID:               conv.FromNullableUUID(record.LogoID),
		Instructions:              conv.FromPGText[string](record.Instructions),
		DefaultEnvironmentID:      conv.FromNullableUUID(record.DefaultEnvironmentID),
		InstallationOverrideURL:   conv.FromPGText[string](record.InstallationOverrideUrl),
		EnvironmentConfigs:        environmentConfigs,
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
		Type:    new("http"),
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

// appendTagsQuery returns mcpURL with a ?tags=<tag> query parameter added. The
// install-page MCP URLs are clean (no existing query), so this encodes the
// single selected tag the same way the runtime ?tags= filter expects.
func appendTagsQuery(mcpURL, tag string) string {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return mcpURL + "?tags=" + url.QueryEscape(tag)
	}
	q := u.Query()
	q.Set("tags", tag)
	u.RawQuery = q.Encode()
	return u.String()
}

// installContext is the resolved subject of an install-page request. When
// resolution comes through mcp_endpoints, mcpServer is set. For toolset-backed
// installs — whether routed via the legacy toolsets.mcp_slug path or via an
// mcp_server with toolset_id set — toolset is also non-nil and drives the
// existing toolset-flavored rendering path. mcpEndpoint is set only on the
// mcp_endpoints resolution path and supplies the public install URL when the
// renderer is Remote-MCP-flavored.
type installContext struct {
	toolset      *toolsets_repo.Toolset
	mcpServer    *mcpservers_repo.McpServer
	mcpEndpoint  *mcpendpoints_repo.McpEndpoint
	organization organizations_repo.OrganizationMetadatum
}

// isPublic returns true when the install page is accessible without auth.
// For toolset-backed installs the existing toolset.McpIsPublic flag wins,
// even when reached via an mcp_server bridge — visibility on the
// mcp_server is irrelevant to a toolset-backed install during the
// dual-source phase. For Remote-MCP-backed installs the mcp_server's own
// visibility flag is authoritative.
func (ic *installContext) isPublic() bool {
	if ic.toolset != nil {
		return ic.toolset.McpIsPublic
	}
	return ic.mcpServer != nil && ic.mcpServer.Visibility == mcpservers.VisibilityPublic
}

// organizationID returns the organization that owns the install. Toolsets
// carry it directly; mcp_servers go through the owning project.
func (ic *installContext) organizationID() string {
	return ic.organization.ID
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

	ic, err := s.resolveInstallContext(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return s.serveNotFoundPage(w, mcpSlug)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load mcp server").Log(ctx, s.logger, attr.SlogToolsetMCPSlug(mcpSlug))
	}

	if !ic.isPublic() {
		// If no auth context, redirect to login page
		if authCtx == nil {
			if s.serverURL != nil {
				loginURL := s.serverURL.String() + "/login"
				http.Redirect(w, r, loginURL, http.StatusFound)
				return nil
			}
			// Fallback if serverURL is nil
			s.logger.InfoContext(ctx, "serving not found page: serverURL is nil", attr.SlogToolsetMCPSlug(mcpSlug))
			return s.serveNotFoundPage(w, mcpSlug)
		}

		// Ought one to check if the user has access to the organization rather than just if the org is active?
		if !authOk || authCtx.ActiveOrganizationID != ic.organizationID() {
			s.logger.InfoContext(ctx, "serving not found page: wrong org or no auth", attr.SlogToolsetMCPSlug(mcpSlug))
			return s.serveNotFoundPage(w, mcpSlug)
		}
	}

	// Load metadata. For mcp_server-backed installs the mcp_server-keyed row
	// wins; if none exists and the server is toolset-backed, fall back to the
	// toolset-keyed row so existing data keeps rendering during the dual-source
	// phase. For pure legacy toolset routing this collapses to the existing
	// toolset-keyed lookup.
	metadataRecord, metadataErr := s.loadInstallPageMetadata(ctx, ic)
	if metadataErr != nil && !errors.Is(metadataErr, pgx.ErrNoRows) {
		s.logger.WarnContext(ctx, "load mcp install page metadata",
			attr.SlogToolsetMCPSlug(mcpSlug),
			attr.SlogError(metadataErr))
	}

	// Honour the installation override URL on either backend.
	if metadataRecord != nil {
		if overrideURL := conv.FromPGText[string](metadataRecord.InstallationOverrideUrl); overrideURL != nil && *overrideURL != "" {
			http.Redirect(w, r, *overrideURL, http.StatusFound)
			return nil
		}
	}

	if ic.toolset != nil {
		return s.renderToolsetInstallPage(ctx, w, ic, mcpSlug, metadataRecord)
	}
	return s.renderRemoteMcpInstallPage(ctx, w, ic, metadataRecord)
}

// resolveInstallContext tries the mcp_endpoints → mcp_server resolution path
// first (via the shared mcpendpoints.BySlugAndCustomDomain helper, mirroring
// mcp.ServePublic's resolution), then falls back to the legacy
// toolsets.mcp_slug lookup so platform-domain install pages keep working for
// customers that pre-date mcp_endpoints. A disabled mcp_server resolves like
// a 404 and is allowed to fall through to the legacy path, again matching
// mcp.ServePublic.
func (s *Service) resolveInstallContext(ctx context.Context, mcpSlug string) (*installContext, error) {
	endpoint, server, err := mcpendpoints.BySlugAndCustomDomain(ctx, s.db, s.logger, mcpSlug)
	var shareErr *oops.ShareableError
	switch {
	case errors.As(err, &shareErr) && shareErr.Code == oops.CodeNotFound:
		// Fall through to legacy toolset lookup.
	case err != nil:
		return nil, fmt.Errorf("resolve mcp endpoint: %w", err)
	default:
		var bridgeToolset *toolsets_repo.Toolset
		if server.ToolsetID.Valid {
			ts, err := s.toolsetRepo.GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
				ID:        server.ToolsetID.UUID,
				ProjectID: server.ProjectID,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// Bridge target gone — render as Remote-MCP-flavored.
			case err != nil:
				return nil, fmt.Errorf("load toolset for mcp_server: %w", err)
			default:
				bridgeToolset = &ts
			}
		}
		org, err := s.lookupInstallOrganization(ctx, bridgeToolset, server)
		if err != nil {
			return nil, err
		}
		return &installContext{
			toolset:      bridgeToolset,
			mcpServer:    server,
			mcpEndpoint:  endpoint,
			organization: org,
		}, nil
	}

	toolset, err := s.loadToolsetFromContextAndSlug(ctx, mcpSlug)
	if err != nil {
		return nil, err
	}
	org, err := s.orgsRepo.GetOrganizationMetadata(ctx, toolset.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("load organization: %w", err)
	}
	return &installContext{
		toolset:      toolset,
		mcpServer:    nil,
		mcpEndpoint:  nil,
		organization: org,
	}, nil
}

// lookupInstallOrganization resolves the organization metadata that owns the
// install. Toolsets carry organization_id directly; mcp_servers don't, so we
// chase the FK through the owning project. toolset takes precedence when both
// are present (toolset-backed mcp_servers share the toolset's org through the
// project FK anyway, so the result is identical).
func (s *Service) lookupInstallOrganization(ctx context.Context, toolset *toolsets_repo.Toolset, server *mcpservers_repo.McpServer) (organizations_repo.OrganizationMetadatum, error) {
	var orgID string
	switch {
	case toolset != nil:
		orgID = toolset.OrganizationID
	case server != nil:
		project, err := s.projectsRepo.GetProjectByID(ctx, server.ProjectID)
		if err != nil {
			return organizations_repo.OrganizationMetadatum{}, fmt.Errorf("load project: %w", err)
		}
		orgID = project.OrganizationID
	default:
		return organizations_repo.OrganizationMetadatum{}, fmt.Errorf("install context has no backend")
	}
	org, err := s.orgsRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return organizations_repo.OrganizationMetadatum{}, fmt.Errorf("load organization: %w", err)
	}
	return org, nil
}

// loadInstallPageMetadata picks the right metadata row for the install
// context. Returns pgx.ErrNoRows when neither backend has metadata so callers
// can distinguish "no branding configured" from a real lookup failure.
func (s *Service) loadInstallPageMetadata(ctx context.Context, ic *installContext) (*repo.McpMetadatum, error) {
	if ic.mcpServer != nil {
		rec, err := s.repo.GetMetadataByMcpServerID(ctx, uuid.NullUUID{UUID: ic.mcpServer.ID, Valid: true})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Fall through to toolset-keyed fallback if applicable.
		case err != nil:
			return nil, fmt.Errorf("get mcp_server-keyed metadata: %w", err)
		default:
			return &rec, nil
		}
	}
	if ic.toolset != nil {
		rec, err := s.repo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: ic.toolset.ID, Valid: true})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, pgx.ErrNoRows
		case err != nil:
			return nil, fmt.Errorf("get toolset-keyed metadata: %w", err)
		}
		return &rec, nil
	}
	return nil, pgx.ErrNoRows
}

func (s *Service) renderToolsetInstallPage(ctx context.Context, w http.ResponseWriter, ic *installContext, mcpSlug string, metadataRecord *repo.McpMetadatum) error {
	toolset := ic.toolset

	// Resolve the effective tool-variations group with the same precedence chain
	// as the runtime (mcp_servers value wins, then the toolset's own column).
	// When a group is resolved we describe the toolset against it so the tools —
	// and the tags we derive below — carry that group's variation overrides,
	// matching exactly what the ?tags= runtime filter would return. A nil result
	// means no explicit group (filtering disabled); the page renders as before.
	var mcpServerGroupID *uuid.UUID
	if ic.mcpServer != nil && ic.mcpServer.ToolVariationsGroupID.Valid {
		mcpServerGroupID = &ic.mcpServer.ToolVariationsGroupID.UUID
	}
	var toolsetGroupID *uuid.UUID
	if toolset.ToolVariationsGroupID.Valid {
		toolsetGroupID = &toolset.ToolVariationsGroupID.UUID
	}
	resolvedGroupID := toolfilter.ResolveGroupID(mcpServerGroupID, toolsetGroupID)

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache, resolvedGroupID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "describe toolset").Log(ctx, s.logger)
	}

	logoAssetURL := s.siteURL.String() + "/external/sticker-logo.png"

	var docsURL string
	var docsText string
	var instructions string
	headerDisplayNames := make(map[string]string)
	variableProvidedBy := make(map[string]string)

	if metadataRecord != nil {
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
		if docs := conv.FromPGText[string](metadataRecord.ExternalDocumentationText); docs != nil {
			docsText = strings.TrimSpace(*docs)
		}
		if inst := conv.FromPGText[string](metadataRecord.Instructions); inst != nil {
			instructions = strings.TrimSpace(*inst)
		}

		// Load header display names from environment entries table and deprecated column
		metadata, err := ToMCPMetadata(ctx, s.repo, *metadataRecord)
		if err != nil {
			s.logger.WarnContext(ctx, "convert metadata to get header display names", attr.SlogToolsetID(toolset.ID.String()), attr.SlogError(err))
		} else {
			for _, config := range metadata.EnvironmentConfigs {
				variableProvidedBy[config.VariableName] = config.ProvidedBy
				if config.HeaderDisplayName != nil {
					headerDisplayNames[config.VariableName] = *config.HeaderDisplayName
				}
			}
		}
	}

	securityMode := s.resolveSecurityMode(toolset, ic.mcpServer)
	securityInputs := s.collectEnvironmentVariables(securityMode, toolsetDetails, headerDisplayNames, variableProvidedBy)

	tools := []toolInfo{}

	for _, toolDesc := range toolsetDetails.Tools {
		if conv.IsProxyTool(toolDesc) {
			info := toolInfo{
				Name:            toolDesc.ExternalMcpToolDefinition.Name,
				Description:     toolDesc.ExternalMcpToolDefinition.Description,
				Title:           "",
				ReadOnlyHint:    false,
				DestructiveHint: false,
				IdempotentHint:  false,
				OpenWorldHint:   false,
				Tags:            toolfilter.EffectiveToolTags(toolDesc),
			}
			applyAnnotations(&info, toolDesc.ExternalMcpToolDefinition.Annotations)
			tools = append(tools, info)
			continue
		}

		baseTool, err := conv.ToBaseTool(toolDesc)
		if err != nil {
			s.logger.WarnContext(ctx, "convert tool to base tool", attr.SlogError(err))
			continue
		}
		info := toolInfo{
			Name:            baseTool.Name,
			Description:     baseTool.Description,
			Title:           "",
			ReadOnlyHint:    false,
			DestructiveHint: false,
			IdempotentHint:  false,
			OpenWorldHint:   false,
			Tags:            toolfilter.EffectiveToolTags(toolDesc),
		}
		applyAnnotations(&info, baseTool.Annotations)
		tools = append(tools, info)
	}

	// Build the selectable scopes from the tools' effective tags (same source as
	// the per-row Tags above, so chips and client-side filtering agree). Filtering
	// is only surfaced when an explicit group is resolved AND it yields at least
	// one tag; an empty group falls back to the unfiltered view (no chips).
	filteringEnabled := false
	scopes := []scopeInfo{}
	if resolvedGroupID != nil {
		tagCounts := map[string]int{}
		for _, t := range tools {
			// Dedupe a tool's tags so a tool that repeats a tag is counted once
			// per scope, matching toolfilter.groupByEffectiveTags.
			seen := map[string]struct{}{}
			for _, tag := range t.Tags {
				if _, ok := seen[tag]; ok {
					continue
				}
				seen[tag] = struct{}{}
				tagCounts[tag]++
			}
		}
		if len(tagCounts) > 0 {
			for _, tag := range slices.Sorted(maps.Keys(tagCounts)) {
				scopes = append(scopes, scopeInfo{Tag: tag, ToolCount: tagCounts[tag]})
			}
			filteringEnabled = true
		}
	}

	mcpURL, err := s.resolveToolsetMCPURL(ctx, *toolset, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve toolset mcp url").Log(ctx, s.logger)
	}

	// Public install slug: prefer the slug from the URL path (which honours the
	// caller's chosen mcp_endpoint when routed via mcp_endpoints) and fall back
	// to the toolset's own mcp_slug for legacy routing.
	publicSlug := mcpSlug
	if publicSlug == "" {
		publicSlug = toolset.McpSlug.String
	}

	return s.writeInstallPage(ctx, w, hostedPageRenderInputs{
		MCPName:          toolset.Name,
		MCPSlug:          publicSlug,
		MCPDescription:   toolset.Description.String,
		MCPURL:           mcpURL,
		SecurityInputs:   securityInputs,
		Tools:            tools,
		LogoAssetURL:     logoAssetURL,
		DocsURL:          docsURL,
		DocsText:         docsText,
		Instructions:     instructions,
		IsPublic:         ic.isPublic(),
		IsOAuth:          securityMode == securityModeOAuth,
		OrgName:          ic.organization.Name,
		FilteringEnabled: filteringEnabled,
		Scopes:           scopes,
	})
}

func (s *Service) renderRemoteMcpInstallPage(ctx context.Context, w http.ResponseWriter, ic *installContext, metadataRecord *repo.McpMetadatum) error {
	mcpServer := ic.mcpServer
	endpoint := ic.mcpEndpoint
	// Construction invariant: the Remote-MCP renderer is only reached via the
	// mcp_endpoints resolution path, so both fields are guaranteed non-nil.
	// Belt-and-suspenders so a future change to the dispatcher can't silently
	// nil-deref through here.
	if mcpServer == nil || endpoint == nil {
		return oops.E(oops.CodeUnexpected, nil, "remote mcp install context missing backend or endpoint").Log(ctx, s.logger)
	}

	logoAssetURL := s.siteURL.String() + "/external/sticker-logo.png"
	var docsURL, docsText, instructions string
	if metadataRecord != nil {
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
		if docs := conv.FromPGText[string](metadataRecord.ExternalDocumentationText); docs != nil {
			docsText = strings.TrimSpace(*docs)
		}
		if inst := conv.FromPGText[string](metadataRecord.Instructions); inst != nil {
			instructions = strings.TrimSpace(*inst)
		}
	}

	mcpURL, err := s.resolveMcpEndpointURL(ctx, endpoint)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve mcp endpoint url").Log(ctx, s.logger, attr.SlogMcpServerID(mcpServer.ID.String()))
	}

	return s.writeInstallPage(ctx, w, hostedPageRenderInputs{
		// Remote-MCP-backed installs don't expose Gram-side env vars or a tools
		// list yet: the page renders the URL + branding only.
		MCPName:        conv.FromPGTextOrEmpty[string](mcpServer.Name),
		MCPSlug:        endpoint.Slug,
		MCPDescription: "",
		MCPURL:         mcpURL,
		SecurityInputs: []securityInput{},
		Tools:          []toolInfo{},
		LogoAssetURL:   logoAssetURL,
		DocsURL:        docsURL,
		DocsText:       docsText,
		Instructions:   instructions,
		IsPublic:       ic.isPublic(),
		// Remote-MCP-backed installs have no toolset, so OAuth is driven solely
		// by the server's user-session issuer (mirrors resolveSecurityMode).
		IsOAuth: mcpServer.UserSessionIssuerID.Valid,
		OrgName: ic.organization.Name,
		// Remote-MCP-backed installs have no tool list, so no filter scopes.
		FilteringEnabled: false,
		Scopes:           nil,
	})
}

// hostedPageRenderInputs gathers the per-install variables passed into the
// page template so the toolset and Remote-MCP renderers share one writer.
type hostedPageRenderInputs struct {
	MCPName        string
	MCPSlug        string
	MCPDescription string
	MCPURL         string
	SecurityInputs []securityInput
	Tools          []toolInfo
	LogoAssetURL   string
	DocsURL        string
	DocsText       string
	Instructions   string
	IsPublic       bool
	// IsOAuth is true when the server authenticates via OAuth (proxy, external,
	// or user-session issuer).
	IsOAuth bool
	OrgName string
	// FilteringEnabled is true when the install context resolves an explicit
	// tool-variations group with at least one tag; it gates all scope UI.
	FilteringEnabled bool
	// Scopes are the selectable filter tags (sorted), each with its tool count.
	Scopes []scopeInfo
}

func (s *Service) writeInstallPage(ctx context.Context, w http.ResponseWriter, in hostedPageRenderInputs) error {
	configSnippetData := jsonSnippetData{
		MCPName:        in.MCPName,
		MCPSlug:        in.MCPSlug,
		MCPDescription: in.MCPDescription,
		MCPURL:         in.MCPURL,
		SecurityInputs: in.SecurityInputs,
		Tools:          in.Tools,
	}

	configSnippetTmpl, err := template.New("config_snippet").Funcs(templatefuncs.FuncMap()).Parse(configSnippetTmplData)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse config snippet template").Log(ctx, s.logger)
	}

	// buildConn assembles the connection strings for a single MCP URL, reusing
	// the same Go builders for every scope so the client never encodes a URL or
	// deep link itself (no drift from the runtime ?tags= behavior).
	buildConn := func(mcpURL string) (scopeConnection, error) {
		snippetData := configSnippetData
		snippetData.MCPURL = mcpURL

		var configSnippet bytes.Buffer
		if err := configSnippetTmpl.Execute(&configSnippet, snippetData); err != nil {
			return scopeConnection{}, fmt.Errorf("execute config snippet template: %w", err)
		}

		cursorURL, err := buildCursorInstallURL(in.MCPName, mcpURL, in.SecurityInputs)
		if err != nil {
			return scopeConnection{}, fmt.Errorf("build cursor install URL: %w", err)
		}
		safeCursorURL, err := safeTemplateURL(cursorURL, "cursor")
		if err != nil {
			return scopeConnection{}, fmt.Errorf("sanitize cursor install URL: %w", err)
		}

		vsCodeURL, err := buildVSCodeInstallURL(in.MCPName, mcpURL, in.SecurityInputs)
		if err != nil {
			return scopeConnection{}, fmt.Errorf("build vscode install URL: %w", err)
		}
		safeVsCodeURL, err := safeTemplateURL(vsCodeURL, "vscode")
		if err != nil {
			return scopeConnection{}, fmt.Errorf("sanitize vscode install URL: %w", err)
		}

		return scopeConnection{
			URL:    mcpURL,
			Config: configSnippet.String(),
			Cursor: safeCursorURL,
			VSCode: safeVsCodeURL,
		}, nil
	}

	defaultConn, err := buildConn(in.MCPURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build default connection").Log(ctx, s.logger)
	}

	// When filtering is enabled, build one connection variant per scope (plus the
	// unfiltered default under the empty-string key) and emit them as JSON for the
	// install-page JS to swap in when a scope chip is selected.
	var scopeVariantsJSON string
	if in.FilteringEnabled {
		variants := map[string]scopeConnection{"": defaultConn}
		for _, scope := range in.Scopes {
			conn, err := buildConn(appendTagsQuery(in.MCPURL, scope.Tag))
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "build scope connection").Log(ctx, s.logger)
			}
			variants[scope.Tag] = conn
		}
		encoded, err := json.Marshal(variants)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "marshal scope variants").Log(ctx, s.logger)
		}
		// Emitted into a data-* attribute (not a <script>), so html/template's
		// attribute auto-escaping handles it — no template.JS / nosec needed.
		scopeVariantsJSON = string(encoded)
	}

	data := hostedPageData{
		jsonSnippetData:   configSnippetData,
		MCPConfig:         defaultConn.Config,
		CursorInstallLink: defaultConn.Cursor,
		VSCodeInstallLink: defaultConn.VSCode,
		OrganizationName:  in.OrgName,
		SiteURL:           s.siteURL.String(),
		ScriptURL:         "/mcp/install-page-" + s.installPageScriptHash + ".js",
		LogoAssetURL:      in.LogoAssetURL,
		DocsURL:           in.DocsURL,
		DocsText:          in.DocsText,
		Instructions:      in.Instructions,
		IsPublic:          in.IsPublic,
		IsOAuth:           in.IsOAuth,
		FilteringEnabled:  in.FilteringEnabled,
		Scopes:            in.Scopes,
		ScopeVariantsJSON: scopeVariantsJSON,
	}

	hostedPageTmpl, err := template.New("hosted_page").Funcs(templatefuncs.FuncMap()).Parse(hostedPageTmplData)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse hosted page template").Log(ctx, s.logger)
	}

	buf := &bytes.Buffer{}
	if err := hostedPageTmpl.Execute(buf, data); err != nil {
		s.logger.ErrorContext(ctx, "execute hosted page template", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "execute hosted page template")
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write(buf.Bytes()); writeErr != nil {
		s.logger.ErrorContext(ctx, "write response body", attr.SlogError(writeErr))
	}

	return nil
}

// resolveToolsetMCPURL builds the public MCP URL for a toolset-backed install
// honouring the URL slug the caller used: when routed through mcp_endpoints
// the URL keeps the endpoint slug; the legacy path keeps toolset.McpSlug.
func (s *Service) resolveToolsetMCPURL(ctx context.Context, toolset toolsets_repo.Toolset, mcpSlug string) (string, error) {
	if mcpSlug == "" {
		return s.resolveMCPURLFromContext(ctx, toolset, s.serverURL.String())
	}
	baseURL := s.serverURL.String() + "/mcp"
	if toolset.CustomDomainID.Valid {
		customDomain, err := s.domainsRepo.GetCustomDomainByID(ctx, toolset.CustomDomainID.UUID)
		if err != nil {
			return "", fmt.Errorf("load custom domain: %w", err)
		}
		baseURL = fmt.Sprintf("https://%s/mcp", customDomain.Domain)
	}
	mcpURL, err := url.JoinPath(baseURL, mcpSlug)
	if err != nil {
		return "", fmt.Errorf("join url path: %w", err)
	}
	return mcpURL, nil
}

// resolveMcpEndpointURL builds the public MCP URL for an mcp_endpoint-routed
// install — custom-domain endpoints render on their own host, platform-domain
// endpoints render under the serverURL.
func (s *Service) resolveMcpEndpointURL(ctx context.Context, endpoint *mcpendpoints_repo.McpEndpoint) (string, error) {
	baseURL := s.serverURL.String() + "/mcp"
	if endpoint.CustomDomainID.Valid {
		customDomain, err := s.domainsRepo.GetCustomDomainByID(ctx, endpoint.CustomDomainID.UUID)
		if err != nil {
			return "", fmt.Errorf("load custom domain: %w", err)
		}
		baseURL = fmt.Sprintf("https://%s/mcp", customDomain.Domain)
	}
	mcpURL, err := url.JoinPath(baseURL, endpoint.Slug)
	if err != nil {
		return "", fmt.Errorf("join url path: %w", err)
	}
	return mcpURL, nil
}

func (s *Service) serveNotFoundPage(w http.ResponseWriter, _ string) error {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(notFoundPageTmplData))

	return nil
}

// InstallPageScriptHash returns the cache-busting hash for the install page script.
func (s *Service) InstallPageScriptHash() string {
	return s.installPageScriptHash
}

// ServeInstallPageScript serves the hosted install page JavaScript with immutable cache headers.
func (s *Service) ServeInstallPageScript(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	hash := chi.URLParam(r, "hash")
	if hash != s.installPageScriptHash {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(s.installPageScriptData); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write script response").Log(ctx, s.logger)
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
	isCustomDomainRequest := customdomains.FromContext(ctx) != nil
	domainID := s.resolveDomainIDFromContext(ctx)
	if domainID != nil {
		toolset, toolsetErr = s.toolsetRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: *domainID, Valid: true},
		})
	}

	// Fall back to slug-only lookup when the request did not arrive through a
	// custom domain. This keeps platform-domain install pages working when the
	// logged-in user's org happens to have a custom domain configured, while
	// still preventing cross-domain toolset leakage for actual custom-domain
	// requests.
	if domainID == nil || (!isCustomDomainRequest && toolsetErr != nil) {
		toolset, toolsetErr = s.toolsetRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
	}

	switch {
	case errors.Is(toolsetErr, pgx.ErrNoRows):
		return nil, fmt.Errorf("%w: %w", errToolsetNotFound, toolsetErr)
	case toolsetErr != nil:
		return nil, fmt.Errorf("lookup toolset: %w", toolsetErr)
	}

	return &toolset, nil
}

// resolveSecurityMode determines the security mode based on toolset and
// mcp_server configuration. OAuth wins regardless of public/private: when an
// OAuth proxy, external OAuth server, or user_session_issuer is attached,
// identity auth is delegated to the OAuth flow and the install instructions
// must not ask the user for an Authorization/GRAM_KEY header. The
// user_session_issuer can sit on the toolset (legacy toolset routing) or on
// the bridging mcp_server (Remote-MCP path), mirroring the public serve path
// which gates OAuth on UserSessionIssuerID.Valid from both sources. server is
// nil when the install is not mcp_server-backed.
func (s *Service) resolveSecurityMode(toolset *toolsets_repo.Toolset, server *mcpservers_repo.McpServer) securityMode {
	oauthRequired := toolset.OauthProxyServerID.Valid ||
		toolset.ExternalOauthServerID.Valid ||
		toolset.UserSessionIssuerID.Valid ||
		(server != nil && server.UserSessionIssuerID.Valid)
	if oauthRequired {
		return securityModeOAuth
	}

	if toolset.McpIsPublic {
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
			if isExplicitlyNotUserProvided(headerDef.Name) {
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
