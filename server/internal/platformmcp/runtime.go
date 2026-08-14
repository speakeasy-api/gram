//nolint:exhaustruct // MCP SDK options deliberately rely on documented zero-value defaults.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	Path         = "/platform-mcp"
	TokenType    = "Bearer"
	MaxBodyBytes = 64 << 10
)

var (
	ErrUnauthorized = errors.New("platform mcp unauthorized")
	ErrForbidden    = errors.New("platform mcp forbidden")
	ErrUnavailable  = errors.New("platform mcp unavailable")
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

// ReadinessRecorder persists authenticated discovery completion for a single
// connection generation. Its input is intentionally the authenticated principal,
// never an OAuth token or raw MCP message.
type ReadinessRecorder interface {
	RecordReady(ctx context.Context, principal Principal, at time.Time) error
}

type Runtime struct {
	authenticator        Authenticator
	gate                 Gate
	authorizer           Authorizer
	protectedResourceURL string
	readiness            ReadinessRecorder
	server               *mcp.Server
}

func NewRuntime(logger *slog.Logger, authenticator Authenticator, gate Gate, authorizer Authorizer, protectedResourceURL, cursorKeyMaterial string, reader Reader, catalog Catalog, registrations *RegistrationService, readiness ReadinessRecorder, setupResources []SetupResource) *Runtime {
	return NewRuntimeWithFeedback(logger, authenticator, gate, authorizer, protectedResourceURL, cursorKeyMaterial, reader, catalog, registrations, readiness, setupResources, nil)
}

func NewRuntimeWithFeedback(logger *slog.Logger, authenticator Authenticator, gate Gate, authorizer Authorizer, protectedResourceURL, cursorKeyMaterial string, reader Reader, catalog Catalog, registrations *RegistrationService, readiness ReadinessRecorder, setupResources []SetupResource, feedback *FeedbackService) *Runtime {
	return NewRuntimeWithLifecycle(logger, authenticator, gate, authorizer, protectedResourceURL, cursorKeyMaterial, reader, catalog, registrations, readiness, setupResources, feedback, nil, nil, CatalogDescriptor{})
}

// NewRuntimeWithLifecycle wires the Platform MCP onboarding lifecycle. Catalogue
// selection remains server-validated: callers receive only search/inspect
// identities and declared configuration fields, never an arbitrary endpoint or
// provider credential.
func NewRuntimeWithLifecycle(logger *slog.Logger, authenticator Authenticator, gate Gate, authorizer Authorizer, protectedResourceURL, cursorKeyMaterial string, reader Reader, catalog Catalog, registrations *RegistrationService, readiness ReadinessRecorder, setupResources []SetupResource, feedback *FeedbackService, onboarding *OnboardingService, distributions *DistributionService, candidate CatalogDescriptor) *Runtime {
	runtime := &Runtime{
		authenticator:        authenticator,
		gate:                 gate,
		authorizer:           authorizer,
		protectedResourceURL: protectedResourceURL,
		readiness:            readiness,
		server:               newServer(reader, catalog, registrations, cursorKeyMaterial, setupResources, feedback, onboarding, distributions, candidate),
	}
	if readiness != nil {
		runtime.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				result, err := next(ctx, method, req)
				if method == "tools/list" && err == nil && result != nil {
					if principal, ok := PrincipalFromContext(ctx); ok {
						if recordErr := runtime.readiness.RecordReady(ctx, principal, time.Now()); recordErr != nil {
							// Discovery succeeded; the idempotent lifecycle projection is
							// best-effort and must not turn an MCP response into a failure.
							logger.WarnContext(ctx, "record platform mcp connection readiness", attr.SlogError(recordErr))
						}
					}
				}
				return result, err
			}
		})
	}
	return runtime
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
		return Principal{}, fmt.Errorf("authenticate platform MCP token: %w", err)
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
