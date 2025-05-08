package chat

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	"goa.design/goa/v3/security"
)

type Service struct {
	openaiAPIKey   string
	auth           *auth.Auth
	openRouter     openrouter.Provisioner
	logger         *slog.Logger
	proxyTransport http.RoundTripper
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, openaiAPIKey string, openRouter openrouter.Provisioner) *Service {
	return &Service{
		openaiAPIKey:   openaiAPIKey,
		auth:           auth.New(logger, db, sessions),
		openRouter:     openRouter,
		logger:         logger,
		proxyTransport: cleanhttp.DefaultPooledTransport(),
	}
}

// HandleCompletion is a simple proxy to the OpenAI API.
// TODO: Security etc
func (s *Service) HandleCompletion(w http.ResponseWriter, r *http.Request) {
	// TODO: Handling security, we can probably factor this out into something smarter like a proxy
	sc := security.APIKeyScheme{
		Name:           auth.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err := s.auth.Authorize(r.Context(), r.Header.Get(auth.SessionHeader), &sc)
	if err != nil {
		sc := security.APIKeyScheme{
			Name:           auth.KeySecurityScheme,
			RequiredScopes: []string{"consumer"},
			Scopes:         []string{},
		}
		ctx, err = s.auth.Authorize(r.Context(), r.Header.Get(auth.APIKeyHeader), &sc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	}

	sc = security.APIKeyScheme{
		Name:           auth.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err = s.auth.Authorize(ctx, r.Header.Get(auth.ProjectHeader), &sc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	_ = ctx

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	target, _ := url.Parse(openrouter.OpenRouterBaseURL)
	apiKey, err := s.openRouter.ProvisionAPIKey(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "error getting openrouter api key falling back to openai", slog.String("error", err.Error()))
		// Fallback to OpenAI API key until fully implemented
		target, _ = url.Parse("https://api.openai.com")
		apiKey = s.openaiAPIKey
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = s.proxyTransport
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
		// Safely join /api (openrouter base path) + /v1/chat/completions
		req.URL.Path = path.Join("/", target.Path, "v1/chat/completions")

		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Handle CORS headers in the response
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Remove any existing CORS headers
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")

		return nil
	}

	proxy.ServeHTTP(w, r)
}
