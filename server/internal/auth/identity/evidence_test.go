package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

const testSessionID = "session_1"

var sessionCreated = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

type evidenceSetup struct {
	googleHD string
	disabled bool
}

func evidenceAdapter(t *testing.T, setup evidenceSetup) *WorkOSAdapter {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user_management/users/user_1/sessions":
			_, _ = w.Write([]byte(`{"data":[{"object":"session","id":"session_0","created_at":"2026-10-02T09:00:00Z"},{"object":"session","id":"` + testSessionID + `","created_at":"` + sessionCreated.Format(time.RFC3339) + `"}],"list_metadata":{}}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer google-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			body := map[string]any{"email": "jane@example.com"}
			if setup.googleHD != "" {
				body["hd"] = setup.googleHD
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := usermanagement.NewClient("test")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()
	enabled := func(context.Context, string) bool { return !setup.disabled }
	return NewWorkOSAdapter(client).WithGoogleUserInfoURL(server.URL + "/userinfo").WithEvidence(EvidenceConfig{
		Logger:        nil,
		MeterProvider: nil,
		Enabled:       enabled,
	})
}

func authResponse(t *testing.T, method usermanagement.AuthenticationMethod, token string) usermanagement.AuthenticateResponse {
	t.Helper()
	var tokens *usermanagement.OAuthTokens
	if token != "" {
		tokens = &usermanagement.OAuthTokens{AccessToken: token, RefreshToken: "", Scopes: nil, ExpiresAt: 0}
	}
	var resp usermanagement.AuthenticateResponse
	resp.User.ID = "user_1"
	resp.User.Email = "jane@example.com"
	resp.User.EmailVerified = true
	resp.OrganizationID = "org_1"
	resp.AccessToken = unsignedJWT(t, map[string]string{"sid": testSessionID})
	resp.AuthenticationMethod = method
	resp.OAuthTokens = tokens
	return resp
}

func unsignedJWT(t *testing.T, claims map[string]string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func want(hostedDomain, tenantID string) SignInEvidence {
	return SignInEvidence{HostedDomain: hostedDomain, TenantID: tenantID, VerifiedEmail: "jane@example.com", AuthenticatedAt: sessionCreated}
}

func TestSignInEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		setup evidenceSetup
		resp  func(t *testing.T) usermanagement.AuthenticateResponse
		want  SignInEvidence
	}{
		{name: "password has no extra evidence", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.Password, "")
		}, want: want("", "")},
		{name: "google workspace account", setup: evidenceSetup{googleHD: "Example.com", disabled: false}, resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.GoogleOAuth, "google-token")
		}, want: want("example.com", "")},
		{name: "personal google account", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.GoogleOAuth, "google-token")
		}, want: want("", "")},
		{name: "google without provider tokens", setup: evidenceSetup{googleHD: "example.com", disabled: false}, resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.GoogleOAuth, "")
		}, want: want("", "")},
		{name: "google token google rejects", setup: evidenceSetup{googleHD: "example.com", disabled: false}, resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.GoogleOAuth, "stale")
		}, want: want("", "")},
		{name: "microsoft work account", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.MicrosoftOAuth, unsignedJWT(t, map[string]string{"tid": "tenant-1", "upn": "jane@Example.com"}))
		}, want: want("example.com", "tenant-1")},
		{name: "microsoft preferred username alone is not enough", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.MicrosoftOAuth, unsignedJWT(t, map[string]string{"tid": "tenant-1", "preferred_username": "jane@example.com"}))
		}, want: want("", "")},
		{name: "microsoft opaque token", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.MicrosoftOAuth, "EwBwA8l6BAAU")
		}, want: want("", "")},
		{name: "sso has no extra evidence", setup: evidenceSetup{googleHD: "", disabled: false}, resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.SSO, "")
		}, want: want("", "")},
		{name: "unverified email is never recorded", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			resp := authResponse(t, usermanagement.SSO, "")
			resp.User.EmailVerified = false
			return resp
		}, want: SignInEvidence{HostedDomain: "", TenantID: "", VerifiedEmail: "", AuthenticatedAt: sessionCreated}},
		{name: "session WorkOS does not list has no authentication time", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			resp := authResponse(t, usermanagement.SSO, "")
			resp.AccessToken = unsignedJWT(t, map[string]string{"sid": "session_unknown"})
			return resp
		}, want: SignInEvidence{HostedDomain: "", TenantID: "", VerifiedEmail: "jane@example.com", AuthenticatedAt: time.Time{}}},
		{name: "enforcement off makes no lookups", setup: evidenceSetup{googleHD: "example.com", disabled: true}, resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(t, usermanagement.GoogleOAuth, "google-token")
		}, want: SignInEvidence{HostedDomain: "", TenantID: "", VerifiedEmail: "jane@example.com", AuthenticatedAt: time.Time{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter := evidenceAdapter(t, tc.setup)
			got := adapter.signInEvidence(ctx, tc.resp(t))
			require.True(t, tc.want.AuthenticatedAt.Equal(got.AuthenticatedAt), "authenticated at %s, want %s", got.AuthenticatedAt, tc.want.AuthenticatedAt)
			got.AuthenticatedAt = tc.want.AuthenticatedAt
			require.Equal(t, tc.want, got)
		})
	}
}
