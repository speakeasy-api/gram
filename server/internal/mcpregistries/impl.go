package mcpregistries

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
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
	"github.com/speakeasy-api/gram/server/internal/mcpregistries/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
	client *http.Client
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("mcp_registries"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/mcpregistries"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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

func (s *Service) ListCatalog(ctx context.Context, payload *gen.ListCatalogPayload) (*gen.ListCatalogResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Fetch all registries from the database
	registries, err := s.repo.ListMCPRegistries(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list registries").Log(ctx, s.logger)
	}

	// If a specific registry is requested, filter to just that one
	if payload.RegistryID != nil {
		registryID, err := uuid.Parse(*payload.RegistryID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid registry_id").Log(ctx, s.logger)
		}

		filtered := make([]repo.ListMCPRegistriesRow, 0, 1)
		for _, r := range registries {
			if r.ID == registryID {
				filtered = append(filtered, r)
				break
			}
		}
		registries = filtered
	}

	// Aggregate servers from all registries
	var allServers []*types.ExternalMCPServer
	for _, registry := range registries {
		servers, err := s.fetchServersFromRegistry(ctx, registry, payload.Search, payload.Cursor)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to fetch servers from registry",
				slog.String("registry_id", registry.ID.String()),
				slog.String("registry_url", registry.Url),
				slog.String("error", err.Error()),
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
		Servers: allServers,
	}, nil
}

// registryListResponse represents the response from the MCP registry API
type registryListResponse struct {
	Servers  []registryServer `json:"servers"`
	Metadata struct {
		Count      int     `json:"count"`
		NextCursor *string `json:"nextCursor"`
	} `json:"metadata"`
}

type registryServer struct {
	Server serverJSON   `json:"server"`
	Meta   responseMeta `json:"_meta"`
}

type serverJSON struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
	Title       *string `json:"title"`
	WebsiteURL  *string `json:"websiteUrl"`
	Icons       []struct {
		URL string `json:"url"`
	} `json:"icons"`
}

type responseMeta struct {
	ID string `json:"id"`
}

func (s *Service) fetchServersFromRegistry(ctx context.Context, registry repo.ListMCPRegistriesRow, search *string, cursor *string) ([]*types.ExternalMCPServer, error) {
	// Build the request URL
	reqURL := fmt.Sprintf("%s/v0/servers?version=latest&limit=50", registry.Url)
	if search != nil && *search != "" {
		reqURL += fmt.Sprintf("&search=%s", *search)
	}
	if cursor != nil && *cursor != "" {
		reqURL += fmt.Sprintf("&cursor=%s", *cursor)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if tenantID := os.Getenv("PULSE_REGISTRY_TENANT"); tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if apiKey := os.Getenv("PULSE_REGISTRY_KEY"); apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var listResp registryListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode registry response: %w", err)
	}

	registryID := registry.ID.String()
	servers := make([]*types.ExternalMCPServer, 0, len(listResp.Servers))
	for _, s := range listResp.Servers {
		server := &types.ExternalMCPServer{
			Name:        s.Server.Name,
			Version:     s.Server.Version,
			Description: s.Server.Description,
			RegistryID:  registryID,
			Title:       s.Server.Title,
		}

		// Get the first icon URL if available
		if len(s.Server.Icons) > 0 {
			server.IconURL = &s.Server.Icons[0].URL
		}

		servers = append(servers, server)
	}

	return servers, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
