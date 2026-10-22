package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/stretchr/testify/require"
)

type failingOAuthClientStore struct {
	platformoauth.Store
	err error
}

func (s failingOAuthClientStore) GetClient(context.Context, string) (platformoauth.Client, error) {
	return platformoauth.Client{}, fmt.Errorf("client lookup: %w", s.err)
}

func TestOAuthHTTPClientFailuresPreserveRegistrationOnOutage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"outage", errors.New("private database failure"), http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable},
		{"missing", platformoauth.ErrNotFound, http.StatusBadRequest, oautherr.CodeInvalidClient},
		{"expired", platformoauth.ErrExpired, http.StatusBadRequest, oautherr.CodeInvalidClient},
		{"mismatch", platformoauth.ErrClientMismatch, http.StatusBadRequest, oautherr.CodeInvalidClient},
		{"revoked", platformoauth.ErrRevoked, http.StatusBadRequest, oautherr.CodeInvalidClient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := newTestOAuthHTTP(t)
			service.store = failingOAuthClientStore{Store: service.store, err: tc.err}
			request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader("grant_type=refresh_token&client_id=test&refresh_token=test"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			service.TokenHandler().ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			var body oautherr.RFC6749Error
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, tc.code, body.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.NotContains(t, response.Body.String(), "private")
		})
	}
}

func TestOAuthHTTPAuthorizeClientLookupOutage(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	service.store = failingOAuthClientStore{Store: service.store, err: errors.New("private database failure")}
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=test&redirect_uri=https%3A%2F%2Fclient.example%2Fcallback&code_challenge="+strings.Repeat("a", 43)+"&code_challenge_method=S256", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	var body oautherr.RFC6749Error
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, oautherr.CodeTemporarilyUnavailable, body.Code)
	require.Empty(t, response.Header().Get("Location"))
	require.NotContains(t, response.Body.String(), "private")
}

func TestOAuthHTTPTokenGateMappings(t *testing.T) {
	t.Parallel()
	for _, grantType := range []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken} {
		for _, gateCase := range []struct {
			name          string
			enabled       bool
			membershipErr error
			gateErr       error
			status        int
			code          string
		}{
			{name: "disabled", status: http.StatusBadRequest, code: oautherr.CodeInvalidGrant},
			{name: "membership lost", enabled: true, membershipErr: ErrForbidden, status: http.StatusBadRequest, code: oautherr.CodeInvalidGrant},
			{name: "outage", gateErr: errors.New("private gate failure"), status: http.StatusServiceUnavailable, code: oautherr.CodeTemporarilyUnavailable},
		} {
			t.Run(grantType+"/"+gateCase.name, func(t *testing.T) {
				t.Parallel()
				service := newTestOAuthHTTP(t)
				store := testStore(t, service)
				now := time.Now()
				connection := platformoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "user:user-1", OrganizationID: "org-1", Generation: "generation-1", AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
				require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: connection.ClientID}))
				require.NoError(t, store.RegisterConnection(t.Context(), connection))
				form := url.Values{"grant_type": {grantType}, "client_id": {connection.ClientID}}
				if grantType == oauthwire.GrantTypeRefreshToken {
					token, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID, "")
					require.NoError(t, err)
					require.NoError(t, store.CreateSession(t.Context(), platformoauth.Session{ID: "session-1", ClientID: connection.ClientID, Connection: connection, JTI: "jti-1", RefreshHash: opaqueHash(token), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}))
					form.Set("refresh_token", token)
				} else {
					code, err := service.credentials.Issue(authorizationCodeCredential, connection.OrganizationID, "")
					require.NoError(t, err)
					verifier := strings.Repeat("v", 43)
					hash := sha256.Sum256([]byte(verifier))
					redirect := "https://client.example/callback"
					require.NoError(t, store.IssueGrant(t.Context(), platformoauth.Grant{Code: code, ClientID: connection.ClientID, Connection: connection, RedirectURI: redirect, CodeChallenge: base64.RawURLEncoding.EncodeToString(hash[:]), ExpiresAt: now.Add(time.Minute)}))
					form.Set("code", code)
					form.Set("redirect_uri", redirect)
					form.Set("code_verifier", verifier)
				}
				service.gate = oauthTestGate{enabled: gateCase.enabled, err: gateCase.gateErr}
				service.authorizer = oauthTestAuthorizer{err: gateCase.membershipErr}
				request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				response := httptest.NewRecorder()
				service.TokenHandler().ServeHTTP(response, request)
				require.Equal(t, gateCase.status, response.Code, response.Body.String())
				var body oautherr.RFC6749Error
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Equal(t, gateCase.code, body.Code)
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			})
		}
	}
}

func TestOAuthHTTPMissingGrantType(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/platform-mcp/token", nil))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
}
