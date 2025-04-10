package chat

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"goa.design/goa/v3/security"
)

type Service struct {
	openaiAPIKey string
	auth         *auth.Auth
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Sessions, openaiAPIKey string) *Service {
	return &Service{
		openaiAPIKey: openaiAPIKey,
		auth:         auth.New(logger, db, sessions),
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
		}
		ctx, err = s.auth.Authorize(r.Context(), r.Header.Get(auth.APIKeyHeader), &sc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	}
	sc = security.APIKeyScheme{
		Name: auth.ProjectSlugSecuritySchema,
	}
	ctx, err = s.auth.Authorize(ctx, r.Header.Get(auth.ProjectHeader), &sc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	target, _ := url.Parse("https://api.openai.com")
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Path = "/v1/chat/completions"
		req.Header.Set("Authorization", "Bearer "+s.openaiAPIKey)
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
