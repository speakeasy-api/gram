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

// ActingSurface names the surface a call arrives through. Audit has to be able
// to tell an agent-driven change from a dashboard change made by the same
// user, and reviewing a runaway agent's burst of activity depends on it.
type ActingSurface string

const (
	// SurfacePlatformMCP is the OAuth-authenticated Platform MCP endpoint.
	SurfacePlatformMCP ActingSurface = "platform_mcp"
	// SurfaceProjectAssistant is the project assistant runtime, which acts
	// under assistant identity and holds no OAuth connection.
	SurfaceProjectAssistant ActingSurface = "project_assistant"
	// SurfaceDashboard is an authenticated dashboard session completing work a
	// Platform MCP flow started, such as a provider setup handoff.
	SurfaceDashboard ActingSurface = "dashboard"
)

// Principal is the identity a Platform MCP call acts under.
//
// ConnectionID and Generation are empty for a surface with no OAuth
// connection. UserID and OrganizationID are always present: authorization,
// idempotency, and audit are keyed on the real user, not on the connection,
// so a call without a connection is attributable exactly as well as one with.
type Principal struct {
	UserID         string
	OrganizationID string
	ConnectionID   string
	Generation     string
	ClientID       string
	Surface        ActingSurface
}

// HasConnection reports whether this principal carries an OAuth connection.
func (p Principal) HasConnection() bool {
	return p.ConnectionID != "" && p.Generation != ""
}

// surface defaults an unset surface to the external endpoint, which is the
// only surface that existed before this field.
func (p Principal) surface() ActingSurface {
	if p.Surface == "" {
		return SurfacePlatformMCP
	}
	return p.Surface
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
	// registrar holds every tool this deployment composed, so an admitted
	// audience can be served from the same pass that built the endpoint.
	registrar            *Registrar
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
	server, registrar := newServer(reader, catalog, registrations, cursorKeyMaterial, setupResources, feedback, onboarding, distributions, candidate)
	runtime := &Runtime{
		authenticator:        authenticator,
		gate:                 gate,
		authorizer:           authorizer,
		protectedResourceURL: protectedResourceURL,
		readiness:            readiness,
		server:               server,
		registrar:            registrar,
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

// ContextWithPrincipal binds an acting identity to a context so tool handlers
// read it with PrincipalFromContext. Exported for the assistant adapter, which
// calls handlers directly rather than through the MCP transport.
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return contextWithPrincipal(ctx, principal)
}

func contextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// AssistantTools returns the descriptors admitted to a project's managed
// assistant. The assistant adapter composes these directly; nothing else in
// the catalogue reaches it.
func (r *Runtime) AssistantTools() []Descriptor {
	if r == nil {
		return nil
	}
	return r.registrar.For(AudienceAssistant)
}
