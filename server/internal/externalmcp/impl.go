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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
	sessions       *sessions.Manager
	registryClient *RegistryClient
	serverURL      *url.URL
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, registryClient *RegistryClient, accessLoader auth.AccessLoader, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("external_mcp"))

	return &Service{
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalmcp"),
		logger:         logger,
		db:             db,
		repo:           repo.New(db),
		auth:           auth.New(logger, db, sessions, accessLoader),
		sessions:       sessions,
		registryClient: registryClient,
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

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "fetch user info").Log(ctx, s.logger)
	}
	if userInfo == nil || !userInfo.Admin {
		return oops.C(oops.CodeForbidden)
	}

	registryID, err := uuid.Parse(payload.RegistryID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid registry_id").Log(ctx, s.logger)
	}

	registry, err := s.repo.GetMCPRegistryByID(ctx, registryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "get registry").Log(ctx, s.logger)
	}

	if err := s.registryClient.ClearCache(ctx, registry.Url); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear registry cache").Log(ctx, s.logger)
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

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "fetch user info").Log(ctx, s.logger)
	}
	if userInfo == nil || !userInfo.Admin {
		return nil, oops.C(oops.CodeForbidden)
	}

	registries, err := s.repo.ListMCPRegistries(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list registries").Log(ctx, s.logger)
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

	// If a specific registry is requested, fetch just that one
	if payload.RegistryID != nil {
		registryID, err := uuid.Parse(*payload.RegistryID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid registry_id").Log(ctx, s.logger)
		}

		registry, err := s.repo.GetMCPRegistryByID(ctx, registryID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.C(oops.CodeNotFound)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get registry").Log(ctx, s.logger)
		}

		servers, err := s.registryClient.ListServers(ctx, Registry{
			ID:  registry.ID,
			URL: registry.Url,
		}, ListServersParams{
			Search: payload.Search,
			Cursor: payload.Cursor,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch servers from registry").Log(ctx, s.logger)
		}

		return &gen.ListCatalogResult{
			Servers:    servers,
			NextCursor: nil, // Pagination not implemented in v0
		}, nil
	}

	// Fetch all registries from the database
	registries, err := s.repo.ListMCPRegistries(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list registries").Log(ctx, s.logger)
	}

	// Aggregate servers from all registries
	var allServers []*types.ExternalMCPServer
	for _, registry := range registries {
		servers, err := s.registryClient.ListServers(ctx, Registry{
			ID:  registry.ID,
			URL: registry.Url,
		}, ListServersParams{
			Search: payload.Search,
			Cursor: payload.Cursor,
		})
		if err != nil {
			s.logger.WarnContext(ctx, "failed to fetch servers from registry",
				attr.SlogMCPRegistryID(registry.ID.String()),
				attr.SlogMCPRegistryURL(registry.Url),
				attr.SlogError(err),
			)
			continue
		}
		allServers = append(allServers, servers...)
	}

	// Cap at 100 servers for v0
	if len(allServers) > 100 {
		allServers = allServers[:100]
	}

	return &gen.ListCatalogResult{
		Servers:    allServers,
		NextCursor: nil, // Pagination not implemented in v0
	}, nil
}

func (s *Service) GetServerDetails(ctx context.Context, payload *gen.GetServerDetailsPayload) (*types.ExternalMCPServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	registryID, err := uuid.Parse(payload.RegistryID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid registry_id").Log(ctx, s.logger)
	}

	registry, err := s.repo.GetMCPRegistryByID(ctx, registryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get registry").Log(ctx, s.logger)
	}

	// Fetch all server details in a single HTTP call
	details, err := s.fetchServerDetails(ctx, registry, payload.ServerSpecifier)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch server details from registry").Log(ctx, s.logger)
	}

	return &types.ExternalMCPServer{
		RegistrySpecifier: details.Name,
		Version:           details.Version,
		Description:       details.Description,
		RegistryID:        registryID.String(),
		RegistryType:      "external",
		Title:             nil, // Not available from details endpoint
		IconURL:           nil, // Not available from details endpoint
		Meta:              nil, // Not available from details endpoint
		Tools:             details.Tools,
		Remotes:           details.Remotes,
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

// fetchServerDetails fetches all server details from the registry in a single HTTP call.
func (s *Service) fetchServerDetails(ctx context.Context, registry repo.GetMCPRegistryByIDRow, serverName string) (*serverDetailsResult, error) {
	reqURL := fmt.Sprintf("%s/v0.1/servers/%s/versions/latest", registry.Url, url.PathEscape(serverName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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
			Name        string `json:"name"`
			Description string `json:"description"`
			Version     string `json:"version"`
			Remotes     []struct {
				URL  string `json:"url"`
				Type string `json:"type"`
			} `json:"remotes"`
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

func (s *Service) Serve(ctx context.Context, payload *gen.ServePayload) (*gen.ServeResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	collection, err := s.repo.GetOrganizationMcpCollectionBySlugAndOrg(ctx, repo.GetOrganizationMcpCollectionBySlugAndOrgParams{
		Slug:           payload.CollectionSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}

	// Get registry namespace for building specifiers
	registry, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, collection.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "get collection registry").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListOrganizationMcpCollectionServerAttachments(ctx, collection.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server attachments").Log(ctx, s.logger)
	}

	servers := make([]*types.ExternalMCPServer, 0, len(toolsets))
	for _, t := range toolsets {
		if !t.McpSlug.Valid {
			continue
		}
		remoteURL := fmt.Sprintf("%s/mcp/%s", s.serverURL.String(), t.McpSlug.String)
		desc := ""
		if t.Description.Valid {
			desc = t.Description.String
		}
		specifier := t.McpSlug.String
		if registry.Namespace != "" {
			specifier = fmt.Sprintf("%s/%s", registry.Namespace, t.McpSlug.String)
		}
		servers = append(servers, &types.ExternalMCPServer{
			RegistrySpecifier: specifier,
			Version:           "1.0.0",
			Description:       desc,
			RegistryID:        collection.ID.String(),
			RegistryType:      "internal",
			Title:             &t.Name,
			IconURL:           nil,
			Meta:              nil,
			Tools:             nil,
			Remotes: []*types.ExternalMCPRemote{{
				URL:           remoteURL,
				TransportType: "streamable-http",
			}},
		})
	}

	return &gen.ServeResult{Servers: servers}, nil
}

func (s *Service) CreateCollection(ctx context.Context, payload *gen.CreateCollectionPayload) (*types.MCPCollection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	collection, err := s.repo.CreateOrganizationMcpCollection(ctx, repo.CreateOrganizationMcpCollectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           payload.Name,
		Description:    pgText(payload.Description),
		Slug:           payload.Slug,
		Visibility:     payload.Visibility,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create collection").Log(ctx, s.logger)
	}

	// Create the registry namespace
	_, err = s.repo.CreateOrganizationMcpCollectionRegistry(ctx, repo.CreateOrganizationMcpCollectionRegistryParams{
		CollectionID: collection.ID,
		Namespace:    payload.McpRegistryNamespace,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create collection registry").Log(ctx, s.logger)
	}

	// Attach initial toolsets if provided
	if len(payload.ToolsetIds) > 0 {
		for _, idStr := range payload.ToolsetIds {
			toolsetID, parseErr := uuid.Parse(idStr)
			if parseErr != nil {
				continue
			}
			_, _ = s.repo.AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
				CollectionID: collection.ID,
				ToolsetID:    toolsetID,
				PublishedBy:  pgText(&authCtx.UserID),
			})
		}
	}

	return toMCPCollection(collection, payload.McpRegistryNamespace), nil
}

func (s *Service) ListCollections(ctx context.Context, payload *gen.ListCollectionsPayload) (*gen.ListCollectionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	collections, err := s.repo.ListOrganizationMcpCollections(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list collections").Log(ctx, s.logger)
	}

	result := make([]*types.MCPCollection, 0, len(collections))
	for _, c := range collections {
		// Fetch registry namespace for each collection
		namespace := ""
		reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, c.ID)
		if err == nil {
			namespace = reg.Namespace
		}
		result = append(result, toMCPCollection(repo.CreateOrganizationMcpCollectionRow(c), namespace))
	}

	return &gen.ListCollectionsResult{Collections: result}, nil
}

func (s *Service) UpdateCollection(ctx context.Context, payload *gen.UpdateCollectionPayload) (*types.MCPCollection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	existing, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if existing.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}

	updated, err := s.repo.UpdateOrganizationMcpCollection(ctx, repo.UpdateOrganizationMcpCollectionParams{
		ID:          collectionID,
		Name:        pgText(payload.Name),
		Description: pgText(payload.Description),
		Visibility:  pgText(payload.Visibility),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update collection").Log(ctx, s.logger)
	}

	namespace := ""
	reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, collectionID)
	if err == nil {
		namespace = reg.Namespace
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(updated), namespace), nil
}

func (s *Service) DeleteCollection(ctx context.Context, payload *gen.DeleteCollectionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	existing, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if existing.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.C(oops.CodeForbidden)
	}

	// Cascade soft-delete to child tables
	_ = s.repo.DeleteOrganizationMcpCollectionRegistriesByID(ctx, collectionID)
	_ = s.repo.DeleteOrganizationMcpCollectionServerAttachmentsByID(ctx, collectionID)

	if err := s.repo.DeleteOrganizationMcpCollection(ctx, collectionID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete collection").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) AttachServer(ctx context.Context, payload *gen.AttachServerPayload) (*types.MCPCollection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	collection, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if collection.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}

	// Reject catalog-installed toolsets
	if installed, checkErr := s.repo.IsToolsetInstalledFromCatalog(ctx, toolsetID); checkErr == nil && installed {
		return nil, oops.E(oops.CodeInvalid, nil, "cannot attach a toolset installed from the catalog").Log(ctx, s.logger)
	}

	_, err = s.repo.AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
		CollectionID: collectionID,
		ToolsetID:    toolsetID,
		PublishedBy:  pgText(&authCtx.UserID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach server").Log(ctx, s.logger)
	}

	namespace := ""
	reg, regErr := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, collectionID)
	if regErr == nil {
		namespace = reg.Namespace
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(collection), namespace), nil
}

func (s *Service) DetachServer(ctx context.Context, payload *gen.DetachServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	existing, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if existing.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.C(oops.CodeForbidden)
	}

	if err := s.repo.DetachServerFromOrganizationMcpCollection(ctx, repo.DetachServerFromOrganizationMcpCollectionParams{
		CollectionID: collectionID,
		ToolsetID:    toolsetID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach server").Log(ctx, s.logger)
	}

	return nil
}

func pgText(s *string) pgtype.Text {
	if s == nil || *s == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func toMCPCollection(c repo.CreateOrganizationMcpCollectionRow, namespace string) *types.MCPCollection {
	var descPtr *string
	if c.Description.Valid {
		descPtr = &c.Description.String
	}
	var nsPtr *string
	if namespace != "" {
		nsPtr = &namespace
	}
	return &types.MCPCollection{
		ID:                   c.ID.String(),
		Name:                 c.Name,
		Description:          descPtr,
		Slug:                 c.Slug,
		McpRegistryNamespace: nsPtr,
		Visibility:           c.Visibility,
	}
}
