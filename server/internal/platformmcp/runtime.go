//nolint:exhaustruct // MCP SDK options deliberately rely on documented zero-value defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	Path         = "/admin-mcp"
	TokenType    = "Bearer"
	MaxBodyBytes = 64 << 10
)

var (
	ErrUnauthorized = errors.New("admin mcp unauthorized")
	ErrForbidden    = errors.New("admin mcp forbidden")
	ErrUnavailable  = errors.New("admin mcp unavailable")
)

type Principal struct {
	UserID         string
	OrganizationID string
	ConnectionID   string
	Generation     string
	ClientID       string
}

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Principal, error)
}

type Gate interface {
	Enabled(ctx context.Context, organizationID string) (bool, error)
}

type Authorizer interface {
	RequireLiveOrgAdmin(ctx context.Context, principal Principal) error
}

type Runtime struct {
	authenticator        Authenticator
	gate                 Gate
	authorizer           Authorizer
	protectedResourceURL string
	server               *mcp.Server
}

func NewRuntime(authenticator Authenticator, gate Gate, authorizer Authorizer, protectedResourceURL string, reader Reader) *Runtime {
	return &Runtime{
		authenticator:        authenticator,
		gate:                 gate,
		authorizer:           authorizer,
		protectedResourceURL: protectedResourceURL,
		server:               newServer(reader),
	}
}

func (r *Runtime) Handler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return r.server
	}, &mcp.StreamableHTTPOptions{
		Stateless:      true,
		JSONResponse:   true,
		SessionTimeout: 0,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		principal, err := r.authenticate(req)
		if err != nil {
			if r.protectedResourceURL != "" {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+r.protectedResourceURL+`"`)
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		enabled, err := r.gate.Enabled(req.Context(), principal.OrganizationID)
		if err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if !enabled {
			http.Error(w, "unavailable", http.StatusForbidden)
			return
		}
		if err := r.authorizer.RequireLiveOrgAdmin(req.Context(), principal); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		req = req.WithContext(contextWithPrincipal(req.Context(), principal))
		req.Body = http.MaxBytesReader(w, req.Body, MaxBodyBytes)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		handler.ServeHTTP(w, req)
	})
}

func (r *Runtime) authenticate(req *http.Request) (Principal, error) {
	if r.authenticator == nil || r.gate == nil || r.authorizer == nil {
		return Principal{}, ErrUnavailable
	}

	kind, token, ok := strings.Cut(req.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(kind, TokenType) || token == "" {
		return Principal{}, ErrUnauthorized
	}

	principal, err := r.authenticator.Authenticate(req.Context(), token)
	if err != nil {
		return Principal{}, fmt.Errorf("authenticate admin token: %w", err)
	}
	if principal.UserID == "" || principal.OrganizationID == "" || principal.ConnectionID == "" || principal.Generation == "" || principal.ClientID == "" {
		return Principal{}, ErrUnauthorized
	}
	return principal, nil
}

type principalContextKey struct{}

func contextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
