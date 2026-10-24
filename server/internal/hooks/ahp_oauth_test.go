package hooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ahpauth "github.com/agenthooksprotocol/go-sdk/auth"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

type ahpOAuthStub struct {
	calls   int
	project string
	token   string
	err     error
}

func (a *ahpOAuthStub) AuthenticateAHP(ctx context.Context, token, project string) (context.Context, error) {
	a.calls++
	a.project = project
	a.token = token
	return ctx, a.err
}

func TestAHPOAuthAdmission(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, bearer, key, project string
		err                        error
		status, calls              int
	}{
		{name: "challenge", status: 401},
		{name: "bound project without header", bearer: "a.b.c", status: 204, calls: 1},
		{name: "selected project header", bearer: "a.b.c", project: "selected", status: 204, calls: 1},
		{name: "invalid JWT cannot use valid key", bearer: "a.b.c", key: "example", project: "chosen", err: platformmcp.ErrUnauthorized, status: 401, calls: 1},
		{name: "explicit key cannot fall back", key: "invalid", project: "chosen", status: 401},
		{name: "key missing project", key: "example", status: 401},
		{name: "invalid key cannot fall back", bearer: "invalid", project: "chosen", status: 401},
		{name: "key prefix cannot fall back", bearer: "gram_local_a.b.c", project: "chosen", status: 401},
		{name: "explicit header is always a key", key: "a.b.c", project: "chosen", status: 401},
		{name: "member revoked", bearer: "a.b.c", err: platformmcp.ErrForbidden, status: 403, calls: 1},
		{name: "project override denied", bearer: "a.b.c", project: "other", err: &platformmcp.ExternalAuthorizationError{RequiredScope: "project:read"}, status: 403, calls: 1},
		{name: "backend unavailable", bearer: "a.b.c", err: errors.New("backend unavailable"), status: 503, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			oauth := &ahpOAuthStub{err: tc.err}
			base, err := url.Parse("https://api.example.test")
			require.NoError(t, err)
			service := (&Service{auth: &ahpTestAuthorizer{authCtx: &contextvalues.AuthContext{}}}).WithAHPOAuth(oauth, base)
			handler := service.ahpAuthenticate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			req := httptest.NewRequest("POST", "/ahp", nil)
			req = req.WithContext(requestorigin.WithContext(req.Context(), requestorigin.Origin{BaseURL: "https://gateway.example.test"}))
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			req.Header.Set("Gram-Key", tc.key)
			req.Header.Set("Gram-Project", tc.project)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, tc.calls, oauth.calls)
			if tc.calls > 0 {
				require.Equal(t, tc.project, oauth.project)
				require.Equal(t, tc.bearer, oauth.token)
			}
			if tc.status == 401 && (tc.calls > 0 || (tc.bearer == "" && tc.key == "")) {
				require.Equal(t, `Bearer resource_metadata="https://gateway.example.test/.well-known/oauth-protected-resource/ahp"`, response.Header().Get("WWW-Authenticate"))
			} else if tc.status == 401 {
				require.Empty(t, response.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

type nativeAHPChallengeProvider struct{ challenge ahpauth.Challenge }

func (p *nativeAHPChallengeProvider) Credential(context.Context, ahpauth.Request) (ahpauth.Credential, error) {
	return ahpauth.Credential{}, nil
}
func (p *nativeAHPChallengeProvider) Challenge(_ context.Context, _ ahpauth.Request, challenge ahpauth.Challenge) (*ahpauth.Credential, error) {
	p.challenge = challenge
	return &ahpauth.Credential{Type: "bearer", Token: "a.b.c"}, nil
}

func TestAHPNativeSDKChallengeRecovery(t *testing.T) {
	t.Parallel()
	oauth := &ahpOAuthStub{}
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	service := (&Service{}).WithAHPOAuth(oauth, base)
	server := httptest.NewServer(service.ahpAuthenticate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	defer server.Close()
	provider := &nativeAHPChallengeProvider{}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"hooks/configure","params":{}}`))
	require.NoError(t, err)
	response, err := ahpauth.Do(server.Client(), request, ahpauth.Request{BackendID: "speakeasy", Destination: server.URL + "/ahp", Purpose: ahpauth.Event}, provider, true)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.Equal(t, http.StatusUnauthorized, provider.challenge.StatusCode)
	require.Equal(t, `Bearer resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/ahp"`, provider.challenge.Header.Get("WWW-Authenticate"))
	require.Equal(t, 1, oauth.calls)
}

func TestAHPOAuthChallengeAcrossOperations(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	service := (&Service{}).WithAHPOAuth(&ahpOAuthStub{}, base)
	handler, err := service.ahpHandler()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, path, body string
		handler          http.Handler
	}{
		{"discovery", "/ahp", `{"jsonrpc":"2.0","id":1,"method":"hooks/configuration","params":{}}`, handler},
		{"event", "/ahp", `{"jsonrpc":"2.0","id":1,"method":"hooks/observe","params":{}}`, handler},
		{"upload", "/ahp/uploads", "attachment", service.ahpUploadHandler()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			tc.handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
			require.Equal(t, http.StatusUnauthorized, response.Code)
			require.Equal(t, `Bearer resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/ahp"`, response.Header().Get("WWW-Authenticate"))
		})
	}
}
