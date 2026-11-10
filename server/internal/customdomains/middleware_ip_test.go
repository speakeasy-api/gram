package customdomains_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCustomDomainIPAllowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		allowlist      []string
		header         []string
		remote         string
		missingContext bool
		preflight      bool
		platform       bool
		want           int
	}{
		{name: "single IP", allowlist: []string{"203.0.113.7"}, header: []string{"203.0.113.7"}, want: http.StatusNoContent},
		{name: "CIDR", allowlist: []string{"203.0.113.0/24"}, header: []string{"203.0.113.7"}, want: http.StatusNoContent},
		{name: "mapped IPv4", allowlist: []string{"203.0.113.0/24"}, header: []string{"::ffff:203.0.113.7"}, want: http.StatusNoContent},
		{name: "denied despite spoofed XFF", allowlist: []string{"203.0.113.7"}, header: []string{"198.51.100.1"}, want: http.StatusForbidden},
		{name: "outside CIDR", allowlist: []string{"203.0.113.0/24"}, header: []string{"198.51.100.1"}, want: http.StatusForbidden},
		{name: "empty allowlist", allowlist: []string{}, header: []string{"198.51.100.1"}, want: http.StatusNoContent},
		{name: "missing header denies spoof", allowlist: []string{"203.0.113.7"}, remote: "192.0.2.1:4321", want: http.StatusForbidden},
		{name: "transport explicitly allowed", allowlist: []string{"192.0.2.1"}, remote: "192.0.2.1:4321", want: http.StatusNoContent},
		{name: "malformed falls back", allowlist: []string{"192.0.2.0/24"}, header: []string{"bad"}, remote: "192.0.2.1:4321", want: http.StatusNoContent},
		{name: "duplicates fall back", allowlist: []string{"192.0.2.1"}, header: []string{"203.0.113.7", "203.0.113.7"}, remote: "192.0.2.1:4321", want: http.StatusNoContent},
		{name: "unresolved denied", allowlist: []string{"203.0.113.7"}, remote: "bad", want: http.StatusForbidden},
		{name: "missing middleware fails closed", allowlist: []string{"203.0.113.7"}, header: []string{"203.0.113.7"}, missingContext: true, want: http.StatusForbidden},
		{name: "invalid stored entry", allowlist: []string{"bad"}, header: []string{"203.0.113.7"}, want: http.StatusForbidden},
		{name: "CORS preflight stays successful", allowlist: []string{"203.0.113.7"}, header: []string{"198.51.100.1"}, preflight: true, want: http.StatusOK},
		{name: "platform unaffected", allowlist: []string{"203.0.113.7"}, header: []string{"198.51.100.1"}, platform: true, want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, instance := newTestInstance(t)
			domain, err := instance.domainsRepo.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
				OrganizationID: "org-ip-test", Domain: "custom.example.com", IpAllowlist: tt.allowlist,
			})
			require.NoError(t, err)
			_, err = instance.domainsRepo.UpdateCustomDomain(ctx, repo.UpdateCustomDomainParams{ID: domain.ID, Verified: true, Activated: true})
			require.NoError(t, err)
			serverURL, err := url.Parse("https://app.example.com")
			require.NoError(t, err)
			logger := testenv.NewLogger(t)
			called := false
			handler := customdomains.Middleware(logger, instance.conn, "prod", serverURL, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			handler = middleware.CORSMiddleware("prod", serverURL.String(), nil, nil)(handler)
			if !tt.missingContext {
				wrap := middleware.PublicHTTPTracing("test")
				handler = wrap(handler)
			}
			method := http.MethodGet
			if tt.preflight {
				method = http.MethodOptions
			}
			host := "custom.example.com"
			if tt.platform {
				host = "app.example.com"
			}
			req := httptest.NewRequest(method, "https://"+host+"/test", nil)
			req.RemoteAddr = tt.remote
			req.Header["X-Client-Ip"] = tt.header
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			req.Header.Set("X-Real-IP", "203.0.113.7")
			req.Header.Set("Origin", serverURL.String())
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			require.Equal(t, tt.want, response.Code, response.Body.String())
			require.Equal(t, tt.want == http.StatusNoContent, called)
		})
	}
}
