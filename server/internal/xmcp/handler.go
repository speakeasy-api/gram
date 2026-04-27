package xmcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// ServeMCP handles DELETE, GET, and POST on /x/mcp/{remoteMcpServerId}. It
// authenticates the caller with a Gram API key, loads the configured Remote
// MCP Server, and delegates the actual forwarding work to the
// remotemcp/proxy package.
func (s *Service) ServeMCP(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	serverIDStr := chi.URLParam(r, "remoteMcpServerId")
	serverID, err := uuid.Parse(serverIDStr)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote mcp server id")
	}

	logger := s.logger.With(attr.SlogRemoteMCPServerID(serverID.String()))

	ctx, err = s.authenticate(ctx, r)
	if err != nil {
		return err
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: serverID.String(), Dimensions: nil}); err != nil {
		return err
	}

	server, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "remote mcp server not found").Log(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server").Log(ctx, logger)
	}

	headers, err := s.newHeadersRepo().ListHeaders(ctx, server.ID, false)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server headers").Log(ctx, logger)
	}

	p := s.buildProxy(logger, &server, headers)

	r = r.WithContext(ctx)

	switch r.Method {
	case http.MethodDelete:
		if err := p.Delete(w, r); err != nil {
			return fmt.Errorf("proxy delete: %w", err)
		}
		return nil
	case http.MethodGet:
		if err := p.Get(w, r); err != nil {
			return fmt.Errorf("proxy get: %w", err)
		}
		return nil
	case http.MethodPost:
		if err := p.Post(w, r); err != nil {
			return fmt.Errorf("proxy post: %w", err)
		}
		return nil
	default:
		// The mux only registers the three supported methods, so this is
		// defensive.
		return oops.E(oops.CodeBadRequest, nil, "unsupported method %s", r.Method)
	}
}

// authenticate extracts the Bearer token from the Authorization header,
// verifies it as a Gram API key, and returns a context carrying the
// authenticated principal plus loaded RBAC grants.
func (s *Service) authenticate(ctx context.Context, r *http.Request) (context.Context, error) {
	token := r.Header.Get("Authorization")
	if token == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	const bearerPrefix = "Bearer "
	if len(token) >= len(bearerPrefix) && strings.EqualFold(token[:len(bearerPrefix)], bearerPrefix) {
		token = token[len(bearerPrefix):]
	}

	scheme := &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         nil,
		RequiredScopes: []string{auth.APIKeyScopeConsumer.String()},
	}

	ctx, err := s.auth.Authorize(ctx, token, scheme)
	if err != nil {
		return ctx, err
	}

	return ctx, nil
}

// buildProxy converts the loaded DB types into the typed configuration
// expected by the remotemcp/proxy package.
func (s *Service) buildProxy(logger *slog.Logger, server *repo.RemoteMcpServer, headers []repo.RemoteMcpServerHeader) *proxy.Proxy {
	configured := make([]proxy.ConfiguredHeader, 0, len(headers))
	for _, h := range headers {
		configured = append(configured, proxy.ConfiguredHeader{
			Name:                   h.Name,
			StaticValue:            h.Value.String,
			ValueFromRequestHeader: h.ValueFromRequestHeader.String,
			IsRequired:             h.IsRequired,
		})
	}

	return &proxy.Proxy{
		HTTPClient:                s.proxyClient,
		Logger:                    logger,
		Tracer:                    s.tracer,
		NonStreamingTimeout:       proxy.DefaultNonStreamingTimeout,
		StreamingTimeout:          proxy.DefaultStreamingTimeout,
		Metrics:                   s.proxyMetrics,
		MaxBufferedBodyBytes:      proxy.DefaultMaxBufferedBodyBytes,
		ServerID:                  server.ID.String(),
		RemoteURL:                 server.Url,
		Headers:                   configured,
		UserRequestInterceptors:   nil,
		RemoteMessageInterceptors: nil,
		ToolsCallRequestInterceptors: []proxy.ToolsCallRequestInterceptor{
			s.toolUsageLimitsInterceptor,
		},
		ToolsCallResponseInterceptors: []proxy.ToolsCallResponseInterceptor{
			s.toolUsageTrackingInterceptor,
		},
		ToolsListRequestInterceptors:  nil,
		ToolsListResponseInterceptors: nil,
	}
}
