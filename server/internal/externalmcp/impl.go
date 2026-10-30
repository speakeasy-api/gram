package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	authz     *authz.Engine
	sessions  *sessions.Manager
	cacheImpl cache.Cache
	catalog   *CatalogService
	// callbackOrigins resolves new-client redirect URIs shown in setup guides.
	callbackOrigins remotesessions.CallbackOrigins
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, cacheImpl cache.Cache, catalog *CatalogService, authzEngine *authz.Engine, callbackOrigin *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("external_mcp"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalmcp"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		sessions:        sessions,
		cacheImpl:       cacheImpl,
		catalog:         catalog,
		callbackOrigins: remotesessions.DefaultCallbackOrigins(callbackOrigin),
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

	if err := s.clearRegistryCache(ctx, registry.Url); err != nil {
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
	if native, ok := reader.(*NativeRegistryReader); ok {
		entry, lookupErr := native.discoveryEntry(ctx, payload.ServerSpecifier)
		if lookupErr != nil {
			err = lookupErr
		} else {
			details, err = decodeDashboardDetails(entry.Data)
		}
	} else {
		err = ErrUnknownRegistrySource
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch server details from registry").LogError(ctx, s.logger)
	}

	registryIDStr := registryID.String()
	return &types.ExternalMCPServer{
		RegistrySpecifier: details.Name,
		Version:           details.Version,
		Description:       details.Description,
		ToolsetID:         nil,
		McpServerID:       nil,
		RegistryID:        &registryIDStr,
		Title:             details.Title,
		IconURL:           details.IconURL,
		Meta:              details.Meta,
		Tools:             details.Tools,
		Remotes:           details.Remotes,
	}, nil
}

func (s *Service) SetCallbackOrigins(origins remotesessions.CallbackOrigins) {
	s.callbackOrigins = origins
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

	// The redirect_uri a newly created client registers, for every provider
	// and slug. remotesessions and the dashboard derive it the same way.
	base, err := s.callbackOrigins.ResolveNewClientBaseURL(ctx, s.db, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organization callback origin").LogError(ctx, s.logger)
	}
	callbackURL := remotesessions.RemoteLoginCallbackURL(s.callbackOrigins.ForClient(base))

	return &gen.GetSetupDocsResult{
		Guides: resolveSetupGuides(registrySpecifier, serverURL, callbackURL),
	}, nil
}

// serverDetailsResult contains all details fetched from the registry for a server.
type serverDetailsResult struct {
	Title       *string
	IconURL     *string
	Meta        map[string]any
	Name        string
	Description string
	Version     string
	Tools       []*types.ExternalMCPTool
	Remotes     []*types.ExternalMCPRemote
}

// clearRegistryCache preserves invalidation of both generic registry cache namespaces.
func (s *Service) clearRegistryCache(ctx context.Context, registryURL string) error {
	var client RegistryClient
	client.listCache = cache.NewTypedObjectCache[CachedListServers](s.logger, s.cacheImpl, cache.SuffixNone)
	client.detailsCache = cache.NewTypedObjectCache[CachedServerDetailsResponse](s.logger, s.cacheImpl, cache.SuffixNone)
	return client.ClearCache(ctx, registryURL)
}

func decodeDashboardDetails(body []byte) (*serverDetailsResult, error) {
	var enrichment struct {
		Server struct {
			Title *string `json:"title"`
			Icons []struct {
				Src string `json:"src"`
			} `json:"icons"`
		} `json:"server"`
		Meta map[string]any `json:"_meta"`
	}
	if err := json.Unmarshal(body, &enrichment); err != nil {
		return nil, fmt.Errorf("decode detail enrichment: %w", err)
	}
	var icon *string
	if len(enrichment.Server.Icons) > 0 {
		icon = &enrichment.Server.Icons[0].Src
	}

	var serverResp serverDetailsEntry
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var remotes []*types.ExternalMCPRemote
	for _, r := range serverResp.Server.Remotes {
		remotes = append(remotes, &types.ExternalMCPRemote{
			URL: r.URL, TransportType: r.Type,
			Headers:   toExternalMCPRemoteHeaders(r.Headers),
			Variables: toExternalMCPRemoteVariables(r.Variables),
		})
	}
	// Catalog records do not publish tool declarations. Live discovery is separate.
	return &serverDetailsResult{
		Title: enrichment.Server.Title, IconURL: icon, Meta: enrichment.Meta,
		Name:        serverResp.Server.Name,
		Description: serverResp.Server.Description,
		Version:     serverResp.Server.Version,
		Tools:       nil,
		Remotes:     remotes,
	}, nil
}
