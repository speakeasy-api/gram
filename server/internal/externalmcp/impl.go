package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_registries/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer         trace.Tracer
	logger         *slog.Logger
	db             *pgxpool.Pool
	repo           *repo.Queries
	auth           *auth.Auth
	authz          *authz.Engine
	sessions       *sessions.Manager
	registryClient *RegistryClient
	catalog        *CatalogService
	serverURL      *url.URL
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, registryClient *RegistryClient, catalog *CatalogService, authzEngine *authz.Engine, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("external_mcp"))

	return &Service{
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalmcp"),
		logger:         logger,
		db:             db,
		repo:           repo.New(db),
		auth:           auth.New(logger, db, sessions, authzEngine),
		authz:          authzEngine,
		sessions:       sessions,
		registryClient: registryClient,
		catalog:        catalog,
		serverURL:      serverURL,
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

func (s *Service) ClearCache(ctx context.Context, payload *gen.ClearCachePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "fetch user info").LogError(ctx, s.logger)
	}
	if userInfo == nil || !userInfo.Admin {
		return oops.C(oops.CodeForbidden)
	}

	registryID, err := uuid.Parse(payload.RegistryID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid registry_id").LogError(ctx, s.logger)
	}

	registry, err := s.repo.GetMCPRegistryByID(ctx, registryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "get registry").LogError(ctx, s.logger)
	}

	if err := s.registryClient.ClearCache(ctx, registry.Url); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear registry cache").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "registry cache cleared",
		attr.SlogMCPRegistryID(registryID.String()),
		attr.SlogMCPRegistryURL(registry.Url),
	)

	return nil
}

func (s *Service) ListRegistries(ctx context.Context, payload *gen.ListRegistriesPayload) (*gen.ListRegistriesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "fetch user info").LogError(ctx, s.logger)
	}
	if userInfo == nil || !userInfo.Admin {
		return nil, oops.C(oops.CodeForbidden)
	}

	registries, err := s.repo.ListMCPRegistries(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list registries").LogError(ctx, s.logger)
	}

	result := make([]*types.MCPRegistry, 0, len(registries))
	for _, r := range registries {
		result = append(result, &types.MCPRegistry{
			ID:   r.ID.String(),
			Name: r.Name,
			URL:  r.Url,
		})
	}

	return &gen.ListRegistriesResult{
		Registries: result,
	}, nil
}

func (s *Service) ListCatalog(ctx context.Context, payload *gen.ListCatalogPayload) (*gen.ListCatalogResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require build read: %w", err)
	}

	var registryID *uuid.UUID
	if payload.RegistryID != nil {
		parsed, err := uuid.Parse(*payload.RegistryID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid registry_id").LogError(ctx, s.logger)
		}
		registryID = &parsed
	}
	if s.catalog == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "catalogue service is not configured").LogError(ctx, s.logger)
	}
	servers, err := s.catalog.List(ctx, payload.Search, registryID)
	if err != nil {
		if errors.Is(err, ErrCatalogSourceNotFound) || errors.Is(err, ErrCatalogSourceDisabled) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "list reviewed catalogue").LogError(ctx, s.logger)
	}

	// Preserve the existing public cap until cursor pagination is upgraded.
	if len(servers) > 100 {
		s.logger.WarnContext(ctx, "catalog result truncated to cap",
			attr.SlogStatsMCPServerCount(len(servers)),
			attr.SlogPaginationLimit(100),
		)
		servers = servers[:100]
	}
	return &gen.ListCatalogResult{Servers: servers, NextCursor: nil}, nil
}

func (s *Service) GetServerDetails(ctx context.Context, payload *gen.GetServerDetailsPayload) (*types.ExternalMCPServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require build read: %w", err)
	}

	registryID, err := uuid.Parse(payload.RegistryID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid registry_id").LogError(ctx, s.logger)
	}

	if s.catalog == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "catalogue service is not configured").LogError(ctx, s.logger)
	}
	// Authorize the source through the same reviewed admission boundary as
	// listCatalog, while retaining the dashboard's existing full-detail shape.
	source, err := s.catalog.Source(ctx, registryID)
	if err != nil {
		if errors.Is(err, ErrCatalogSourceNotFound) || errors.Is(err, ErrCatalogSourceDisabled) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "resolve reviewed catalogue source").LogError(ctx, s.logger)
	}
	reader, err := s.catalog.ReaderFor(source)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve catalogue source adapter").LogError(ctx, s.logger)
	}

	var details *serverDetailsResult
	if _, ok := reader.(*RegistryClient); ok {
		// Preserve the dashboard's existing Pulse detail projection, which includes
		// every remote and the Pulse-specific per-remote tool metadata.
		details, err = s.fetchServerDetails(ctx, source.Registry, payload.ServerSpecifier)
	} else {
		// Source-specific adapters normalize their selected detail result through
		// the shared catalogue boundary, keeping reader selection aligned with
		// source admission as new certified sources are enabled.
		registryDetails, detailErr := s.catalog.Details(ctx, registryID, payload.ServerSpecifier, nil)
		if detailErr != nil {
			err = detailErr
		} else {
			details = serverDetailsResultFromRegistryDetails(registryDetails)
		}
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch server details from registry").LogError(ctx, s.logger)
	}

	registryIDStr := registryID.String()
	return &types.ExternalMCPServer{
		RegistrySpecifier:                   details.Name,
		Version:                             details.Version,
		Description:                         details.Description,
		ToolsetID:                           nil,
		McpServerID:                         nil,
		RegistryID:                          &registryIDStr,
		OrganizationMcpCollectionRegistryID: nil,
		Title:                               nil,
		IconURL:                             nil,
		Meta:                                nil,
		Tools:                               details.Tools,
		Remotes:                             details.Remotes,
	}, nil
}

func (s *Service) GetSetupDocs(ctx context.Context, payload *gen.GetSetupDocsPayload) (*gen.GetSetupDocsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require project read: %w", err)
	}

	registrySpecifier := strings.TrimSpace(conv.PtrValOr(payload.RegistrySpecifier, ""))
	serverURL := strings.TrimSpace(conv.PtrValOr(payload.ServerURL, ""))
	if registrySpecifier == "" && serverURL == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one of server_url or registry_specifier must be provided").LogError(ctx, s.logger)
	}

	// The one redirect_uri for every provider and slug. remotesessions/challenge.go,
	// oauth/impl.go, and the dashboard derive it the same way.
	callbackURL := s.serverURL.JoinPath("mcp", "remote_login_callback").String()

	return &gen.GetSetupDocsResult{
		Guides: resolveSetupGuides(registrySpecifier, serverURL, callbackURL),
	}, nil
}

// serverDetailsResult contains all details fetched from the registry for a server.
type serverDetailsResult struct {
	Name        string
	Description string
	Version     string
	Tools       []*types.ExternalMCPTool
	Remotes     []*types.ExternalMCPRemote
}

func serverDetailsResultFromRegistryDetails(details *ServerDetails) *serverDetailsResult {
	if details == nil {
		return nil
	}

	var tools []*types.ExternalMCPTool
	if details.Tools != nil {
		tools = make([]*types.ExternalMCPTool, 0, len(details.Tools))
		for i := range details.Tools {
			tool := &details.Tools[i]
			tools = append(tools, &types.ExternalMCPTool{
				Name:        &tool.Name,
				Description: &tool.Description,
				InputSchema: tool.InputSchema,
				Annotations: tool.Annotations,
			})
		}
	}

	var remotes []*types.ExternalMCPRemote
	if details.RemoteURL != "" {
		remotes = []*types.ExternalMCPRemote{{
			URL:           details.RemoteURL,
			TransportType: string(details.TransportType),
			Headers:       toExternalMCPRemoteHeaders(details.Headers),
			Variables:     toExternalMCPRemoteVariables(details.Variables),
		}}
	}

	return &serverDetailsResult{
		Name:        details.Name,
		Description: details.Description,
		Version:     details.Version,
		Tools:       tools,
		Remotes:     remotes,
	}
}

// fetchServerDetails fetches all server details from the registry in a single HTTP call.
func (s *Service) fetchServerDetails(ctx context.Context, registry Registry, serverName string) (*serverDetailsResult, error) {
	registryURL, err := reviewedRegistryDetailsURL(registry.URL)
	if err != nil {
		return nil, err
	}
	requestURL := registryURL.JoinPath("v0.1", "servers", url.PathEscape(serverName), "versions", "latest")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if s.registryClient.backend.Match(req) {
		if err := s.registryClient.backend.Authorize(req); err != nil {
			return nil, fmt.Errorf("authorize request: %w", err)
		}
	}

	resp, err := s.registryClient.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	type remoteMeta struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
			Annotations map[string]any  `json:"annotations"`
		} `json:"tools"`
	}
	var serverResp struct {
		Server struct {
			Name        string             `json:"name"`
			Description string             `json:"description"`
			Version     string             `json:"version"`
			Remotes     []serverRemoteJSON `json:"remotes"`
		} `json:"server"`
		Meta struct {
			Version struct {
				FirstRemote  remoteMeta `json:"remotes[0]"`
				SecondRemote remoteMeta `json:"remotes[1]"`
				ThirdRemote  remoteMeta `json:"remotes[2]"`
				FourthRemote remoteMeta `json:"remotes[3]"`
				FifthRemote  remoteMeta `json:"remotes[4]"`
			} `json:"com.pulsemcp/server-version"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert remotes and find preferred remote index (streamable-http > sse)
	var remotes []*types.ExternalMCPRemote
	preferredIndex := -1
	foundStreamable := false
	for i, r := range serverResp.Server.Remotes {
		remotes = append(remotes, &types.ExternalMCPRemote{
			URL:           r.URL,
			TransportType: r.Type,
			Headers:       toExternalMCPRemoteHeaders(r.Headers),
			Variables:     toExternalMCPRemoteVariables(r.Variables),
		})
		// Prefer first streamable-http; fall back to first sse.
		// Can't break early because we need all remotes in the slice.
		if r.Type == "streamable-http" && !foundStreamable {
			preferredIndex = i
			foundStreamable = true
		} else if r.Type == "sse" && preferredIndex == -1 {
			preferredIndex = i
		}
	}

	// Get tools from the preferred remote (matching registryclient.go behavior)
	var selectedRemote remoteMeta
	switch preferredIndex {
	case 0:
		selectedRemote = serverResp.Meta.Version.FirstRemote
	case 1:
		selectedRemote = serverResp.Meta.Version.SecondRemote
	case 2:
		selectedRemote = serverResp.Meta.Version.ThirdRemote
	case 3:
		selectedRemote = serverResp.Meta.Version.FourthRemote
	case 4:
		selectedRemote = serverResp.Meta.Version.FifthRemote
	}

	// Convert tools
	var tools []*types.ExternalMCPTool
	for _, t := range selectedRemote.Tools {
		tools = append(tools, &types.ExternalMCPTool{
			Name:        &t.Name,
			Description: &t.Description,
			InputSchema: t.InputSchema,
			Annotations: t.Annotations,
		})
	}

	return &serverDetailsResult{
		Name:        serverResp.Server.Name,
		Description: serverResp.Server.Description,
		Version:     serverResp.Server.Version,
		Tools:       tools,
		Remotes:     remotes,
	}, nil
}
