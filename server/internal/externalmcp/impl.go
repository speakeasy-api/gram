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
	registryClient *RegistryClient
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, registryClient *RegistryClient) *Service {
	logger = logger.With(attr.SlogComponent("externalmcp"))

	return &Service{
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalmcp"),
		logger:         logger,
		db:             db,
		repo:           repo.New(db),
		auth:           auth.New(logger, db, sessions),
		registryClient: registryClient,
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

	details, err := s.registryClient.GetServerDetails(ctx, Registry{
		ID:  registry.ID,
		URL: registry.Url,
	}, payload.ServerSpecifier, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch server details from registry").Log(ctx, s.logger)
	}

	// Build the remotes array from the full details response
	// We need to re-fetch the raw response to get all remotes since GetServerDetails
	// only returns the selected remote. Let's use a different approach - fetch the list
	// endpoint filtered by server name to get remotes.
	// Actually, looking at the registry client, the serverDetailsJSON.Remotes field
	// has all remotes. We need to fetch the raw details ourselves.

	// Make a separate call to get all remotes
	allRemotes, err := s.fetchAllRemotes(ctx, registry, payload.ServerSpecifier)
	if err != nil {
		// Log warning but continue - we still have the basic details
		s.logger.WarnContext(ctx, "failed to fetch all remotes for server",
			attr.SlogError(err),
		)
	}

	return &types.ExternalMCPServer{
		RegistrySpecifier: details.Name,
		Version:           details.Version,
		Description:       details.Description,
		RegistryID:        registryID.String(),
		Remotes:           allRemotes,
	}, nil
}

// fetchAllRemotes fetches all available remotes for a server from the registry.
func (s *Service) fetchAllRemotes(ctx context.Context, registry repo.GetMCPRegistryByIDRow, serverName string) ([]*types.ExternalMCPRemote, error) {
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var serverResp struct {
		Server struct {
			Remotes []struct {
				URL  string `json:"url"`
				Type string `json:"type"`
			} `json:"remotes"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var remotes []*types.ExternalMCPRemote
	for _, r := range serverResp.Server.Remotes {
		remotes = append(remotes, &types.ExternalMCPRemote{
			URL:           r.URL,
			TransportType: r.Type,
		})
	}

	return remotes, nil
}
