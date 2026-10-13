package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

func evidenceAdapter(t *testing.T, factors string, googleHD string) *WorkOSAdapter {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user_management/users/user_1/auth_factors":
			_, _ = w.Write([]byte(`{"data":[` + factors + `],"list_metadata":{}}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer google-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			body := map[string]any{"email": "jane@example.com"}
			if googleHD != "" {
				body["hd"] = googleHD
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
	return NewWorkOSAdapter(client).WithGoogleUserInfoURL(server.URL + "/userinfo")
}

func authResponse(method usermanagement.AuthenticationMethod, token string) usermanagement.AuthenticateResponse {
	var tokens *usermanagement.OAuthTokens
	if token != "" {
		tokens = &usermanagement.OAuthTokens{AccessToken: token, RefreshToken: "", Scopes: nil, ExpiresAt: 0}
	}
	var resp usermanagement.AuthenticateResponse
	resp.User.ID = "user_1"
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

func TestSignInEvidence(t *testing.T) {
	t.Parallel()
	const totp = `{"id":"f1","object":"authentication_factor","type":"totp"}`
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		factors string
		hd      string
		resp    func(t *testing.T) usermanagement.AuthenticateResponse
		want    SignInEvidence
	}{
		{name: "password with an enrolled totp factor", factors: totp, resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.Password, "")
		}, want: SignInEvidence{MFAVerified: true, HostedDomain: "", TenantID: ""}},
		{name: "password without a factor", resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.Password, "")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "", TenantID: ""}},
		{name: "google workspace account", hd: "Example.com", resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.GoogleOAuth, "google-token")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "example.com", TenantID: ""}},
		{name: "personal google account", resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.GoogleOAuth, "google-token")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "", TenantID: ""}},
		{name: "google without provider tokens", hd: "example.com", resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.GoogleOAuth, "")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "", TenantID: ""}},
		{name: "google token google rejects", hd: "example.com", resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.GoogleOAuth, "stale")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "", TenantID: ""}},
		{name: "microsoft work account", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(usermanagement.MicrosoftOAuth, unsignedJWT(t, map[string]string{"tid": "tenant-1", "upn": "jane@Example.com"}))
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "example.com", TenantID: "tenant-1"}},
		{name: "microsoft preferred username", resp: func(t *testing.T) usermanagement.AuthenticateResponse {
			t.Helper()
			return authResponse(usermanagement.MicrosoftOAuth, unsignedJWT(t, map[string]string{"tid": "tenant-1", "preferred_username": "jane@example.com"}))
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "example.com", TenantID: "tenant-1"}},
		{name: "microsoft opaque token", resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.MicrosoftOAuth, "EwBwA8l6BAAU")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "", TenantID: ""}},
		{name: "sso has no extra evidence", factors: totp, resp: func(*testing.T) usermanagement.AuthenticateResponse {
			return authResponse(usermanagement.SSO, "")
		}, want: SignInEvidence{MFAVerified: false, HostedDomain: "", TenantID: ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter := evidenceAdapter(t, tc.factors, tc.hd)
			require.Equal(t, tc.want, adapter.signInEvidence(ctx, tc.resp(t)))
		})
	}
}
