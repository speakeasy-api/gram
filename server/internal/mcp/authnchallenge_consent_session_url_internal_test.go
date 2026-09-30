package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestConsentSessionCanonicalAuthority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		server, host string
		match        bool
	}{
		{"https://app.example.com:443", "app.example.com", true},
		{"https://app.example.com", "app.example.com:443", true},
		{"https://APP.example.com:443", "app.EXAMPLE.com", true},
		{"https://app.example.com:8443", "app.example.com:8443", true},
		{"https://app.example.com:8443", "app.example.com", false},
		{"https://app.example.com", "app.example.com:8443", false},
		{"https://app.example.com", "app.example.com:80", false},
		{"http://localhost:80", "LOCALHOST", true},
		{"http://localhost", "localhost:80", true},
		{"http://[::1]:80", "[::1]", true},
		{"http://localhost:3000", "localhost:3000", true},
		{"http://localhost:3000", "localhost", false},
		{"https://app.example.com", "other.example.com", false},
		{"https://app.example.com", "app.example.com:", false},
		{"https://app.example.com", "app.example.com:65536", false},
		{"https://app.example.com", "app.example.com.", false},
		{"https://app.example.com", "user@app.example.com", false},
		{"https://app.example.com", " app.example.com", false},
	} {
		t.Run(tc.server+"/"+tc.host, func(t *testing.T) {
			t.Parallel()
			s := browserTestService(t)
			server, err := url.Parse(tc.server)
			require.NoError(t, err)
			s.serverURL, s.siteURL = server, server
			s.consentSessionCache = cache.NewTypedObjectCache[consentSessionHandoff](s.logger, testenv.NewMemoryCache(), cache.SuffixNone)
			r := httptest.NewRequest(http.MethodGet, "https://app.example.com/oauth/agent-consent-session?ticket=missing", nil)
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Host", server.Host)
			err = s.HandleConsentSessionHandoff(httptest.NewRecorder(), r)
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied)
			if tc.match {
				require.Equal(t, oops.CodeUnauthorized, denied.Code, "matching authority must reach ticket lookup")
			} else {
				require.Equal(t, oops.CodeNotFound, denied.Code, "forwarded host must not authorize a mismatched request")
			}
		})
	}
}

func TestConsentSessionNormalizedOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scheme, host, origin, fetchSite string
		allowed                         bool
	}{
		{"https", "app.example.com:443", "https://app.example.com", "", true},
		{"https", "app.example.com", "https://app.example.com:443", "", true},
		{"https", "APP.example.com", "https://app.EXAMPLE.com", "", true},
		{"https", "app.example.com:8443", "https://app.example.com:8443", "", true},
		{"https", "app.example.com:8443", "https://app.example.com", "", false},
		{"https", "app.example.com", "https://app.example.com:8443", "", false},
		{"http", "localhost:80", "http://LOCALHOST", "", true},
		{"http", "localhost", "http://localhost:80", "", true},
		{"http", "[::1]:80", "http://[::1]", "", true},
		{"https", "app.example.com", "http://app.example.com", "", false},
		{"https", "app.example.com", "https://other.example.com", "same-origin", false},
		{"https", "app.example.com:443", "https://app.example.com", "cross-site", false},
		{"https", "app.example.com:443", "https://app.example.com", "same-site", false},
		{"https", "app.example.com:443", "https://app.example.com", "same-origin", true},
		{"https", "app.example.com", "null", "", false},
		{"https", "app.example.com", "https://user@app.example.com", "", false},
		{"https", "app.example.com", "https://app.example.com/path", "", false},
		{"https", "app.example.com", "https://app.example.com:", "", false},
		{"https", "app.example.com", "", "", true},
	} {
		t.Run(tc.host+"/"+tc.origin+"/"+tc.fetchSite, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "https://app.example.com/oauth/agent-consent-session", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			err := checkConsentSessionOrigin(r, tc.scheme)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.host, r.Host)
			require.Equal(t, tc.origin, r.Header.Get("Origin"), "validation must not mutate the request")
		})
	}
}
func TestConsentSessionReturnURLSafety(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		origin string
		safe   bool
	}{
		{"https://mcp.example.com", true},
		{"https://mcp.example.com:8443", true},
		{"", true}, // Legacy state falls back to the validated canonical server.
		{"http://localhost:3000", true},
		{"http://127.0.0.1:3000", true},
		{"http://[::1]:3000", true},
		{"http://mcp.example.com", false},
		{"http://localhost.example.com", false},
		{"//mcp.example.com", false},
		{"https://user:password@mcp.example.com", false},
		{"https://mcp.example.com#fragment", false},
		{"https://mcp.example.com:65536", false},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			t.Parallel()
			canonical, err := url.Parse("https://app.example.com")
			require.NoError(t, err)
			s := &Service{serverURL: canonical, siteURL: canonical}
			endpoint := &ResolvedMcpEndpoint{RouteBase: "mcp", Slug: "test"}
			state := AuthnChallengeState{ID: "state+with&reserved=characters", Endpoint: EndpointRef{BaseURL: tc.origin}}
			target, err := s.consentSessionReturnURL(endpoint, state)
			if tc.safe {
				require.NoError(t, err)
				u, err := url.Parse(target)
				require.NoError(t, err)
				require.Equal(t, state.ID, u.Query().Get("state"), "validation must retain the required state query")
				require.Equal(t, "/mcp/test/connect", u.Path)
				origin := tc.origin
				if origin == "" {
					origin = canonical.String()
				}
				require.Equal(t, origin, u.Scheme+"://"+u.Host)
			} else {
				require.Error(t, err)
				require.Empty(t, target)
				// No cache or authorization dependencies: an unsafe cached return
				// origin must fail before issuing a ticket, even with HTTPS config.
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodPost, "https://app.example.com/connect/remote-session", nil)
				require.Error(t, s.startConsentSessionHandoff(w, r, endpoint, state))
				require.Empty(t, w.Body.String())
				require.Empty(t, w.Header().Get("Location"))
			}
		})
	}
}
func TestConsentSessionURLSafety(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url  string
		safe bool
	}{
		{"https://app.example.com", true},
		{"https://app.example.com:8443/dashboard", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1:3000", true},
		{"http://[::1]:3000", true},
		{"http://app.example.com", false},
		{"http://localhost.example.com", false},
		{"http://127.0.0.1.example.com", false},
		{"http://192.168.1.1", false},
		{"//app.example.com", false},
		{"/dashboard", false},
		{"javascript:alert(1)", false},
		{"https://user:password@app.example.com", false},
		{"https://app.example.com?next=http://other.example.com", false},
		{"https://app.example.com?", false},
		{"https://app.example.com#login", false},
		{"https://app.example.com:0", false},
		{"https://app.example.com:65536", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tc.url)
			require.NoError(t, err)
			err = validateConsentSessionURL(u)
			if tc.safe {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	require.Error(t, validateConsentSessionURL(nil))
}

func TestConsentSessionRejectsUnsafeConfiguredURLsBeforeHandoff(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"server", "site"} {
		for _, raw := range []string{"http://app.example.com", "https://user@app.example.com", "/relative"} {
			t.Run(field+"/"+raw, func(t *testing.T) {
				t.Parallel()
				safe, err := url.Parse("https://app.example.com")
				require.NoError(t, err)
				unsafe, err := url.Parse(raw)
				require.NoError(t, err)
				s := &Service{serverURL: safe, siteURL: safe}
				if field == "server" {
					s.serverURL = unsafe
				} else {
					s.siteURL = unsafe
				}
				// Deliberately omit all dependencies: rejection must precede ticket
				// storage, session authentication, link rendering and confirmation.
				for _, method := range []string{http.MethodGet, http.MethodPost} {
					r := httptest.NewRequest(method, "https://app.example.com/oauth/agent-consent-session?ticket=test", nil)
					w := httptest.NewRecorder()
					err := s.HandleConsentSessionHandoff(w, r)
					var denied *oops.ShareableError
					require.ErrorAs(t, err, &denied)
					require.Equal(t, oops.CodeUnavailable, denied.Code)
					require.Empty(t, w.Body.String())
					require.Empty(t, w.Header().Get("Location"))
				}
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodPost, "https://mcp.example.com/connect/remote-session", nil)
				err = s.startConsentSessionHandoff(w, r, nil, AuthnChallengeState{})
				var denied *oops.ShareableError
				require.ErrorAs(t, err, &denied)
				require.Equal(t, oops.CodeUnavailable, denied.Code)
				require.Empty(t, w.Body.String(), "must not emit an insecure ticket link")
			})
		}
	}
}
