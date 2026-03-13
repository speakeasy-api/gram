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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	repoTypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, registryClient *RegistryClient, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("externalmcp"))

	return &Service{
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalmcp"),
		logger:         logger,
		db:             db,
		repo:           repo.New(db),
		auth:           auth.New(logger, db, sessions),
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

func (s *Service) CreatePeer(ctx context.Context, payload *gen.CreatePeerPayload) (*types.PeeredOrganization, error) {
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

	if authCtx.ActiveOrganizationID == payload.SubOrganizationID {
		return nil, oops.E(oops.CodeBadRequest, nil, "cannot peer an organization with itself").Log(ctx, s.logger)
	}

	peer, err := s.repo.CreatePeer(ctx, repo.CreatePeerParams{
		SuperOrganizationID: authCtx.ActiveOrganizationID,
		SubOrganizationID:   payload.SubOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create peer").Log(ctx, s.logger)
	}

	return &types.PeeredOrganization{
		ID:                  peer.ID.String(),
		SuperOrganizationID: peer.SuperOrganizationID,
		SubOrganizationID:   peer.SubOrganizationID,
		SubOrganizationName: nil,
		SubOrganizationSlug: nil,
		CreatedAt:           peer.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

func (s *Service) ListPeers(ctx context.Context, payload *gen.ListPeersPayload) (*gen.ListPeersResult, error) {
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

	peers, err := s.repo.ListPeers(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list peers").Log(ctx, s.logger)
	}

	result := make([]*types.PeeredOrganization, 0, len(peers))
	for _, p := range peers {
		result = append(result, &types.PeeredOrganization{
			ID:                  p.ID.String(),
			SuperOrganizationID: p.SuperOrganizationID,
			SubOrganizationID:   p.SubOrganizationID,
			SubOrganizationName: &p.SubOrganizationName,
			SubOrganizationSlug: &p.SubOrganizationSlug,
			CreatedAt:           p.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return &gen.ListPeersResult{
		Peers: result,
	}, nil
}

func (s *Service) DeletePeer(ctx context.Context, payload *gen.DeletePeerPayload) error {
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

	if err := s.repo.DeletePeer(ctx, repo.DeletePeerParams{
		SuperOrganizationID: authCtx.ActiveOrganizationID,
		SubOrganizationID:   payload.SubOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete peer").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) Grant(ctx context.Context, payload *gen.GrantPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
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

	// Verify the caller's project owns this registry
	if authCtx.ProjectID == nil || !registry.ProjectID.Valid || registry.ProjectID.UUID != *authCtx.ProjectID {
		return oops.C(oops.CodeForbidden)
	}

	// Verify the target org is a peered sub of the caller's org
	isPeer, err := s.repo.IsPeer(ctx, repo.IsPeerParams{
		SuperOrganizationID: authCtx.ActiveOrganizationID,
		SubOrganizationID:   payload.OrganizationID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check peer").Log(ctx, s.logger)
	}
	if !isPeer {
		return oops.E(oops.CodeForbidden, nil, "target organization is not a peered sub-organization").Log(ctx, s.logger)
	}

	if _, err := s.repo.CreateRegistryGrant(ctx, repo.CreateRegistryGrantParams{
		RegistryID:     registryID,
		OrganizationID: payload.OrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "create registry grant").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) RevokeGrant(ctx context.Context, payload *gen.RevokeGrantPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
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

	// Verify the caller's project owns this registry
	if authCtx.ProjectID == nil || !registry.ProjectID.Valid || registry.ProjectID.UUID != *authCtx.ProjectID {
		return oops.C(oops.CodeForbidden)
	}

	if err := s.repo.DeleteRegistryGrant(ctx, repo.DeleteRegistryGrantParams{
		RegistryID:     registryID,
		OrganizationID: payload.OrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete registry grant").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) Publish(ctx context.Context, payload *gen.PublishPayload) (*types.MCPRegistry, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Parse and validate toolset IDs
	toolsetIDs := make([]uuid.UUID, 0, len(payload.ToolsetIds))
	for _, idStr := range payload.ToolsetIds {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
		}
		toolsetIDs = append(toolsetIDs, id)
	}

	var projectID uuid.NullUUID
	if authCtx.ProjectID != nil {
		projectID = uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	registry, err := txRepo.CreateInternalRegistry(ctx, repo.CreateInternalRegistryParams{
		Name:           payload.Name,
		Slug:           conv.ToPGText(payload.Slug),
		Visibility:     payload.Visibility,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create internal registry").Log(ctx, s.logger)
	}

	if err := txRepo.SetRegistryToolsets(ctx, repo.SetRegistryToolsetsParams{
		RegistryID: registry.ID,
		ToolsetIds: toolsetIDs,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set registry toolsets").Log(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, s.logger)
	}

	return registryRowToType(registry.ID, registry.Name, registry.Url, registry.Slug, registry.Source, registry.Visibility, registry.OrganizationID), nil
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

	if !registry.Url.Valid {
		return oops.E(oops.CodeBadRequest, nil, "internal registries have no cache to clear").Log(ctx, s.logger)
	}

	if err := s.registryClient.ClearCache(ctx, registry.Url.String); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear registry cache").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "registry cache cleared",
		attr.SlogMCPRegistryID(registryID.String()),
		attr.SlogMCPRegistryURL(registry.Url.String),
	)

	return nil
}

func (s *Service) ListRegistries(ctx context.Context, payload *gen.ListRegistriesPayload) (*gen.ListRegistriesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	registries, err := s.repo.ListRegistriesForOrganization(ctx, conv.ToPGText(authCtx.ActiveOrganizationID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list registries for organization").Log(ctx, s.logger)
	}

	result := make([]*types.MCPRegistry, 0, len(registries))
	for _, r := range registries {
		result = append(result, registryRowToType(r.ID, r.Name, r.Url, r.Slug, r.Source, r.Visibility, r.OrganizationID))
	}

	return &gen.ListRegistriesResult{
		Registries: result,
	}, nil
}

func (s *Service) Serve(ctx context.Context, payload *gen.ServePayload) (*gen.ServeResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	registry, err := s.repo.GetMCPRegistryBySlug(ctx, conv.ToPGText(payload.RegistrySlug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get registry by slug").Log(ctx, s.logger)
	}

	// Authorization: registry must be public, owned by caller's org, or have a grant
	if registry.Visibility != string(repoTypes.VisibilityPublic) {
		if !registry.OrganizationID.Valid || registry.OrganizationID.String != authCtx.ActiveOrganizationID {
			hasGrant, err := s.repo.CheckRegistryGrant(ctx, repo.CheckRegistryGrantParams{
				RegistryID:     registry.ID,
				OrganizationID: authCtx.ActiveOrganizationID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "check registry grant").Log(ctx, s.logger)
			}
			if !hasGrant {
				return nil, oops.C(oops.CodeForbidden)
			}
		}
	}

	// Branch on source type
	if registry.Source.Valid && registry.Source.String == string(repoTypes.RegistrySourceInternal) {
		return s.serveInternalRegistry(ctx, registry, payload)
	}

	// External registry: use existing RegistryClient flow
	if !registry.Url.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "registry has no external URL").Log(ctx, s.logger)
	}

	servers, err := s.registryClient.ListServers(ctx, Registry{
		ID:  registry.ID,
		URL: registry.Url.String,
	}, ListServersParams{
		Search: payload.Search,
		Cursor: payload.Cursor,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "fetch servers from registry").Log(ctx, s.logger)
	}

	return &gen.ServeResult{
		Servers:    servers,
		NextCursor: nil,
	}, nil
}

// serveInternalRegistry queries linked toolsets and marshals them into the ExternalMCPServer format,
// matching the structure returned by PulseMCP for external registries.
func (s *Service) serveInternalRegistry(ctx context.Context, registry repo.GetMCPRegistryBySlugRow, payload *gen.ServePayload) (*gen.ServeResult, error) {
	links, err := s.repo.ListRegistryToolsetLinks(ctx, registry.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list registry toolset links").Log(ctx, s.logger)
	}

	var servers []*types.ExternalMCPServer
	for _, link := range links {
		toolset, err := s.repo.GetToolsetForServe(ctx, link.ToolsetID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get toolset for serve").Log(ctx, s.logger)
		}

		// Apply search filter
		if payload.Search != nil && *payload.Search != "" {
			if !strings.Contains(strings.ToLower(toolset.Name), strings.ToLower(*payload.Search)) {
				continue
			}
		}

		description := ""
		if toolset.Description.Valid {
			description = toolset.Description.String
		}

		// Load full toolset details via mv.DescribeToolset
		described, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), nil)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "describe toolset %s", toolset.Slug).Log(ctx, s.logger)
		}

		// Convert types.Tool into meta tools and ExternalMCPTool
		metaTools := make([]map[string]any, 0, len(described.Tools))
		mcpTools := make([]*types.ExternalMCPTool, 0, len(described.Tools))
		for _, tool := range described.Tools {
			name, desc, schema, annotations := extractToolFields(tool)
			metaTools = append(metaTools, map[string]any{
				"name":        name,
				"description": desc,
				"inputSchema": json.RawMessage(schema),
				"annotations": annotations,
			})
			mcpTools = append(mcpTools, &types.ExternalMCPTool{
				Name:        &name,
				Description: &desc,
				InputSchema: []byte(schema),
				Annotations: annotations,
			})
		}

		// Build remote URL for this toolset's MCP endpoint
		var remotes []*types.ExternalMCPRemote
		if toolset.McpSlug.Valid && toolset.McpEnabled {
			remoteURL := fmt.Sprintf("%s/mcp/%s", s.serverURL.String(), toolset.McpSlug.String)
			remotes = []*types.ExternalMCPRemote{
				{
					URL:           remoteURL,
					TransportType: "streamable-http",
				},
			}
		}

		meta := map[string]any{
			"ai.getgram/server": map[string]any{
				"visitorsEstimateMostRecentWeek": 0,
				"visitorsEstimateLastFourWeeks":  0,
				"visitorsEstimateTotal":          0,
				"isOfficial":                     false,
			},
			"ai.getgram/server-version": map[string]any{
				"source":      string(repoTypes.RegistrySourceInternal),
				"status":      "active",
				"publishedAt": described.CreatedAt,
				"updatedAt":   described.UpdatedAt,
				"isLatest":    true,
				"remotes[0]": map[string]any{
					"auth":  nil,
					"tools": metaTools,
				},
			},
		}

		if !toolset.McpSlug.Valid {
			return nil, oops.E(oops.CodeUnexpected, nil, "toolset %s missing mcp_slug", toolset.Slug).Log(ctx, s.logger)
		}

		servers = append(servers, &types.ExternalMCPServer{
			RegistrySpecifier: toolset.McpSlug.String,
			Version:           "1.0.0",
			Description:       description,
			RegistryID:        registry.ID.String(),
			Title:             &toolset.Name,
			IconURL:           nil,
			Meta:              meta,
			Tools:             mcpTools,
			Remotes:           remotes,
		})
	}

	return &gen.ServeResult{
		Servers:    servers,
		NextCursor: nil,
	}, nil
}

// extractToolFields extracts name, description, schema, and annotations from a types.Tool
// regardless of which variant (HTTP, Function, ExternalMCP) is populated.
func extractToolFields(tool *types.Tool) (name, description, schema string, annotations map[string]any) {
	annotations = make(map[string]any)

	switch {
	case tool.HTTPToolDefinition != nil:
		t := tool.HTTPToolDefinition
		name = t.Name
		description = t.Description
		schema = t.Schema
		if t.Annotations != nil {
			annotations = annotationsToMap(t.Annotations)
		}
	case tool.FunctionToolDefinition != nil:
		t := tool.FunctionToolDefinition
		name = t.Name
		description = t.Description
		schema = t.Schema
		if t.Annotations != nil {
			annotations = annotationsToMap(t.Annotations)
		}
	case tool.ExternalMcpToolDefinition != nil:
		t := tool.ExternalMcpToolDefinition
		name = t.Name
		description = t.Description
		schema = t.Schema
	case tool.PromptTemplate != nil:
		t := tool.PromptTemplate
		name = t.Name
		description = t.Description
		schema = t.Schema
	}

	return name, description, schema, annotations
}

func annotationsToMap(a *types.ToolAnnotations) map[string]any {
	m := make(map[string]any)
	if a.ReadOnlyHint != nil {
		m["readOnlyHint"] = *a.ReadOnlyHint
	}
	if a.DestructiveHint != nil {
		m["destructiveHint"] = *a.DestructiveHint
	}
	if a.IdempotentHint != nil {
		m["idempotentHint"] = *a.IdempotentHint
	}
	if a.OpenWorldHint != nil {
		m["openWorldHint"] = *a.OpenWorldHint
	}
	return m
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
		return nil, oops.E(oops.CodeUnexpected, err, "get registry").Log(ctx, s.logger)
	}

	// Authorization: registry must be public, owned by caller's org, or have a grant
	if registry.Visibility != string(repoTypes.VisibilityPublic) {
		if !registry.OrganizationID.Valid || registry.OrganizationID.String != authCtx.ActiveOrganizationID {
			hasGrant, err := s.repo.CheckRegistryGrant(ctx, repo.CheckRegistryGrantParams{
				RegistryID:     registry.ID,
				OrganizationID: authCtx.ActiveOrganizationID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "check registry grant").Log(ctx, s.logger)
			}
			if !hasGrant {
				return nil, oops.C(oops.CodeForbidden)
			}
		}
	}

	// Branch on source type
	if registry.Source.Valid && registry.Source.String == string(repoTypes.RegistrySourceInternal) {
		return s.getInternalServerDetails(ctx, registry, payload.ServerSpecifier)
	}

	// External registry: fetch from remote
	details, err := s.fetchServerDetails(ctx, registry, payload.ServerSpecifier)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "fetch server details from registry").Log(ctx, s.logger)
	}

	return &types.ExternalMCPServer{
		RegistrySpecifier: details.Name,
		Version:           details.Version,
		Description:       details.Description,
		RegistryID:        registryID.String(),
		Title:             nil,
		IconURL:           nil,
		Meta:              nil,
		Tools:             details.Tools,
		Remotes:           details.Remotes,
	}, nil
}

func (s *Service) getInternalServerDetails(ctx context.Context, registry repo.GetMCPRegistryByIDRow, serverSpecifier string) (*types.ExternalMCPServer, error) {
	toolset, err := s.repo.GetRegistryToolsetByMCPSlug(ctx, repo.GetRegistryToolsetByMCPSlugParams{
		RegistryID: registry.ID,
		McpSlug:    conv.ToPGText(serverSpecifier),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get registry toolset by slug").Log(ctx, s.logger)
	}

	if !toolset.McpSlug.Valid {
		return nil, oops.E(oops.CodeUnexpected, nil, "toolset %s missing mcp_slug", toolset.Slug).Log(ctx, s.logger)
	}

	description := ""
	if toolset.Description.Valid {
		description = toolset.Description.String
	}

	described, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "describe toolset %s", toolset.Slug).Log(ctx, s.logger)
	}

	metaTools := make([]map[string]any, 0, len(described.Tools))
	mcpTools := make([]*types.ExternalMCPTool, 0, len(described.Tools))
	for _, tool := range described.Tools {
		name, desc, schema, annotations := extractToolFields(tool)
		metaTools = append(metaTools, map[string]any{
			"name":        name,
			"description": desc,
			"inputSchema": json.RawMessage(schema),
			"annotations": annotations,
		})
		mcpTools = append(mcpTools, &types.ExternalMCPTool{
			Name:        &name,
			Description: &desc,
			InputSchema: []byte(schema),
			Annotations: annotations,
		})
	}

	var remotes []*types.ExternalMCPRemote
	if toolset.McpEnabled {
		remoteURL := fmt.Sprintf("%s/mcp/%s", s.serverURL.String(), toolset.McpSlug.String)
		remotes = []*types.ExternalMCPRemote{
			{
				URL:           remoteURL,
				TransportType: "streamable-http",
			},
		}
	}

	meta := map[string]any{
		"ai.getgram/server": map[string]any{
			"visitorsEstimateMostRecentWeek": 0,
			"visitorsEstimateLastFourWeeks":  0,
			"visitorsEstimateTotal":          0,
			"isOfficial":                     false,
		},
		"ai.getgram/server-version": map[string]any{
			"source":      string(repoTypes.RegistrySourceInternal),
			"status":      "active",
			"publishedAt": described.CreatedAt,
			"updatedAt":   described.UpdatedAt,
			"isLatest":    true,
			"remotes[0]": map[string]any{
				"auth":  nil,
				"tools": metaTools,
			},
		},
	}

	return &types.ExternalMCPServer{
		RegistrySpecifier: toolset.McpSlug.String,
		Version:           "1.0.0",
		Description:       description,
		RegistryID:        registry.ID.String(),
		Title:             &toolset.Name,
		IconURL:           nil,
		Meta:              meta,
		Tools:             mcpTools,
		Remotes:           remotes,
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
	if !registry.Url.Valid {
		return nil, fmt.Errorf("registry has no external URL")
	}
	reqURL := fmt.Sprintf("%s/v0.1/servers/%s/versions/latest", registry.Url.String, url.PathEscape(serverName))

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

func registryRowToType(id uuid.UUID, name string, urlField, slugField, sourceField pgtype.Text, visibility string, orgID pgtype.Text) *types.MCPRegistry {
	return &types.MCPRegistry{
		ID:             id.String(),
		Name:           name,
		URL:            conv.FromPGText[string](urlField),
		Slug:           conv.FromPGText[string](slugField),
		Source:         conv.FromPGText[string](sourceField),
		Visibility:     &visibility,
		OrganizationID: conv.FromPGText[string](orgID),
	}
}
