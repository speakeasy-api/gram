package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// runSecurity applies security to a request and returns what it reported sending.
func runSecurity(t *testing.T, policy *guardian.Policy, security []*HTTPToolSecurity, system, user map[string]string) (bool, int, *httptest.ResponseRecorder, *toolconfig.CredentialReport) {
	t.Helper()
	report := &toolconfig.CredentialReport{}
	env := toolconfig.ToolCallEnv{SystemEnv: toolconfig.CIEnvFrom(system), UserConfig: toolconfig.CIEnvFrom(user), Credentials: report}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/resource", nil)
	w := httptest.NewRecorder()
	var status int
	ok := processSecurity(t.Context(), testenv.NewLogger(t), policy, req, w, &status, newTestToolDescriptor(), &HTTPToolCallPlan{Security: security}, testenv.NewMemoryCache(), env, "https://api.example", tm.HTTPLogAttributes{})
	return ok, status, w, report
}

func unsafePolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return policy
}

var machineSecurity = &HTTPToolSecurity{
	Key:          "machine",
	Type:         NullString{Value: "oauth2", Valid: true},
	OAuthTypes:   []string{"client_credentials"},
	OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"https://unused.example/token"}}`),
	EnvVariables: []string{"MACHINE_CLIENT_ID", "MACHINE_CLIENT_SECRET", "MACHINE_TOKEN_URL", "MACHINE_ACCESS_TOKEN"},
}

// clientGrantMessages pins each failure's exact text; both address failures read the same.
var clientGrantMessages = map[toolconfig.ClientGrantFailure]string{
	toolconfig.ClientGrantUnavailable:     "This tool couldn't sign in: its sign-in service didn't answer or had an error. If it keeps happening, check its sign-in address if you set it yourself, or ask your administrator.",
	toolconfig.ClientGrantRejected:        "This tool couldn't sign in: its sign-in service refused the request. If you didn't set its sign-in details yourself, ask your administrator to check the tool's sign-in setup.",
	toolconfig.ClientGrantBlocked:         "This tool couldn't sign in: its sign-in address can't be used. If you didn't set it yourself, ask your administrator to check it.",
	toolconfig.ClientGrantAddressNotFound: "This tool couldn't sign in: its sign-in address can't be used. If you didn't set it yourself, ask your administrator to check it.",
	toolconfig.ClientGrantMisconfigured:   "This tool couldn't sign in: its sign-in service answered in a way Speakeasy didn't expect. If you didn't set its sign-in address yourself, ask your administrator to check it.",
}

// A failed token request reports why and which secret it used, and adds nothing to the response headers.
func TestClientGrantFailureIsReported(t *testing.T) {
	t.Parallel()

	answering := func(status int) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		t.Cleanup(server.Close)
		return server.URL
	}
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	redirectToBlocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.10/token", http.StatusFound)
	}))
	t.Cleanup(redirectToBlocked.Close)
	missingHost := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}})

	for _, tc := range []struct {
		name     string
		tokenURL string
		blocking bool
		options  []func(*guardian.Policy)
		want     toolconfig.ClientGrantFailure
	}{
		{name: "missing host", tokenURL: "http://token.example.invalid/token", options: []func(*guardian.Policy){guardian.WithResolver(missingHost)}, want: toolconfig.ClientGrantAddressNotFound},
		{name: "redirect to a blocked address keeps its old reading", tokenURL: redirectToBlocked.URL, options: []func(*guardian.Policy){guardian.WithBlockedCIDRBlocks("192.0.2.0/24")}, want: toolconfig.ClientGrantMisconfigured},
		{name: "refused", tokenURL: answering(http.StatusUnauthorized), want: toolconfig.ClientGrantRejected},
		{name: "server error", tokenURL: answering(http.StatusInternalServerError), want: toolconfig.ClientGrantUnavailable},
		{name: "rate limited", tokenURL: answering(http.StatusTooManyRequests), want: toolconfig.ClientGrantUnavailable},
		{name: "bad request", tokenURL: answering(http.StatusBadRequest), want: toolconfig.ClientGrantRejected},
		{name: "down", tokenURL: down.URL, want: toolconfig.ClientGrantUnavailable},
		{name: "wrong url", tokenURL: answering(http.StatusNotFound), want: toolconfig.ClientGrantMisconfigured},
		{name: "blocked address", tokenURL: answering(http.StatusOK), blocking: true, want: toolconfig.ClientGrantBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
			if !tc.blocking {
				var err error
				policy, err = guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, tc.options...)
				require.NoError(t, err)
			}
			ok, status, w, report := runSecurity(t, policy, []*HTTPToolSecurity{machineSecurity},
				map[string]string{"MACHINE_CLIENT_ID": "id", "MACHINE_CLIENT_SECRET": "secret", "MACHINE_TOKEN_URL": tc.tokenURL}, nil)
			require.False(t, ok)
			require.Equal(t, http.StatusUnauthorized, status)
			require.Equal(t, tc.want, report.GrantFailure)
			require.Equal(t, []toolconfig.AppliedCredential{{Variable: "MACHINE_CLIENT_SECRET", System: true, ClientGrant: true, Carrier: "Authorization"}}, report.Sent)
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Equal(t, []string{"Content-Type"}, keys(w.Header()))
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))
			require.JSONEq(t, `{"error":`+strconv.Quote(clientGrantMessages[tc.want])+`}`, w.Body.String())
			parsed, err := url.Parse(tc.tokenURL)
			require.NoError(t, err)
			require.NotContains(t, w.Body.String(), parsed.Hostname())
			require.NotContains(t, w.Body.String(), "192.0.2.10")
		})
	}
}

// A grant that fails without the proxy answering 401 still reports why, unless another Authorization was sent.
func TestClientGrantMisconfigurationIsReported(t *testing.T) {
	t.Parallel()

	notAToken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not a token"))
	}))
	t.Cleanup(notAToken.Close)

	for _, tc := range []struct {
		name     string
		tokenURL string
	}{
		{name: "token endpoint answers without a token", tokenURL: notAToken.URL},
		{name: "token url is malformed", tokenURL: "http://[::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			system := map[string]string{"MACHINE_CLIENT_ID": "id", "MACHINE_CLIENT_SECRET": "secret", "MACHINE_TOKEN_URL": tc.tokenURL}
			ok, _, w, report := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{machineSecurity}, system, nil)
			require.True(t, ok)
			require.Empty(t, keys(w.Header()))
			require.Equal(t, toolconfig.ClientGrantMisconfigured, report.GrantFailure)
			require.Equal(t, []toolconfig.AppliedCredential{{Variable: "MACHINE_CLIENT_SECRET", System: true, ClientGrant: true, Carrier: "Authorization"}}, report.Sent)

			system["AUTHORIZATION"] = "Bearer saved"
			ok, _, _, report = runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{machineSecurity}, system, nil)
			require.True(t, ok)
			require.Empty(t, report.GrantFailure)
			require.Equal(t, []toolconfig.AppliedCredential{{Variable: "authorization", System: true, ClientGrant: false, Carrier: "Authorization"}}, report.Sent)
		})
	}
}

// A caller's access token skips the grant, so the saved secret was never sent.
func TestCallerAccessTokenSkipsClientGrant(t *testing.T) {
	t.Parallel()

	ok, _, _, report := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{machineSecurity},
		map[string]string{"MACHINE_CLIENT_ID": "id", "MACHINE_CLIENT_SECRET": "secret"}, map[string]string{"MACHINE_ACCESS_TOKEN": "caller-token"})
	require.True(t, ok)
	require.Empty(t, report.GrantFailure)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "MACHINE_ACCESS_TOKEN", System: false, ClientGrant: false, Carrier: "Authorization"}}, report.Sent)
}

// A scheme's Authorization wins over a saved system Authorization, which is then not reported.
func TestSchemeAuthorizationHidesSystemAuthorization(t *testing.T) {
	t.Parallel()

	bearer := &HTTPToolSecurity{Key: "bearer", Type: NullString{Value: "http", Valid: true}, Scheme: NullString{Value: "bearer", Valid: true}, EnvVariables: []string{"CALLER_TOKEN"}}
	ok, _, _, report := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{bearer},
		map[string]string{"AUTHORIZATION": "Bearer saved"}, map[string]string{"CALLER_TOKEN": "caller"})
	require.True(t, ok)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "CALLER_TOKEN", System: false, ClientGrant: false, Carrier: "Authorization"}}, report.Sent)

	ok, _, _, report = runSecurity(t, unsafePolicy(t), nil, map[string]string{"AUTHORIZATION": "Bearer saved"}, nil)
	require.True(t, ok)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "authorization", System: true, ClientGrant: false, Carrier: "Authorization"}}, report.Sent)
}

// A value missing from the caller's layer is reported as the system one that was sent.
func TestSystemFallbackIsReportedAsSystem(t *testing.T) {
	t.Parallel()

	apiKey := &HTTPToolSecurity{Key: "api_key", Type: NullString{Value: "apiKey", Valid: true}, Name: NullString{Value: "X-Api-Key", Valid: true}, Placement: NullString{Value: "header", Valid: true}, EnvVariables: []string{"API_KEY"}}
	ok, _, _, report := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{apiKey}, map[string]string{"API_KEY": "saved"}, nil)
	require.True(t, ok)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "API_KEY", System: true, ClientGrant: false, Carrier: "X-Api-Key"}}, report.Sent)
}

func keys(header http.Header) []string {
	out := make([]string, 0, len(header))
	for k := range header {
		out = append(out, k)
	}
	return out
}

func recordingTokenEndpoint(t *testing.T, accessToken string) (string, func() []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var forms []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		copied := make(url.Values, len(r.PostForm))
		for k, v := range r.PostForm {
			copied[k] = append([]string(nil), v...)
		}
		mu.Lock()
		forms = append(forms, copied)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","expires_in":60}`))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/token", func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		out := make([]url.Values, len(forms))
		copy(out, forms)
		return out
	}
}

func TestClientCredentialsTokenURLPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("system client ignores user token URL header", func(t *testing.T) {
		t.Parallel()

		defaultURL, defaultForms := recordingTokenEndpoint(t, "default-tok")
		userURL, userForms := recordingTokenEndpoint(t, "user-tok")
		sec := &HTTPToolSecurity{
			Key:          "acme",
			Type:         NullString{Value: "oauth2", Valid: true},
			OAuthTypes:   []string{"client_credentials"},
			OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"` + defaultURL + `"}}`),
			EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
		}

		ok, _, _, _ := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{sec},
			map[string]string{"ACME_CLIENT_ID": "sys-id", "ACME_CLIENT_SECRET": "sys-secret"},
			map[string]string{"ACME_TOKEN_URL": userURL})
		require.True(t, ok)
		require.Empty(t, userForms())
		require.Len(t, defaultForms(), 1)
		require.Equal(t, "sys-id", defaultForms()[0].Get("client_id"))
		require.Equal(t, "sys-secret", defaultForms()[0].Get("client_secret"))
	})

	t.Run("system client ignores tool call argument token URL", func(t *testing.T) {
		t.Parallel()

		defaultURL, defaultForms := recordingTokenEndpoint(t, "default-tok")
		argURL, argForms := recordingTokenEndpoint(t, "arg-tok")
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(upstream.Close)

		plan := &HTTPToolCallPlan{
			ServerEnvVar:     "ACME_SERVER_URL",
			DefaultServerUrl: NullString{Value: upstream.URL, Valid: true},
			Security: []*HTTPToolSecurity{{
				Key:          "acme",
				Type:         NullString{Value: "oauth2", Valid: true},
				OAuthTypes:   []string{"client_credentials"},
				OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"` + defaultURL + `"}}`),
				EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
			}},
			SecurityScopes:     map[string][]string{},
			Method:             http.MethodGet,
			Path:               "/widgets",
			Schema:             []byte(`{"type":"object"}`),
			HeaderParams:       map[string]*HTTPParameter{},
			QueryParams:        map[string]*HTTPParameter{},
			PathParams:         map[string]*HTTPParameter{},
			RequestContentType: NullString{Value: "application/json", Valid: true},
		}

		body, err := json.Marshal(map[string]any{
			"environmentVariables": map[string]string{"ACME_TOKEN_URL": argURL},
		})
		require.NoError(t, err)

		proxy := NewToolProxy(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), testenv.NewMemoryCache(), unsafePolicy(t), nil, nil, nil)
		err = proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader(body), toolconfig.ToolCallEnv{
			SystemEnv:   toolconfig.CIEnvFrom(map[string]string{"ACME_CLIENT_ID": "sys-id", "ACME_CLIENT_SECRET": "sys-secret"}),
			SavedConfig: toolconfig.NewCaseInsensitiveEnv(),
			UserConfig:  toolconfig.NewCaseInsensitiveEnv(),
		}, NewHTTPToolCallPlan(newTestToolDescriptor(), plan), tm.HTTPLogAttributes{})
		require.NoError(t, err)
		require.Empty(t, argForms())
		require.Len(t, defaultForms(), 1)
		require.Equal(t, "sys-secret", defaultForms()[0].Get("client_secret"))
	})

	t.Run("partial user client falls back to configured token URL", func(t *testing.T) {
		t.Parallel()

		sysURL, sysForms := recordingTokenEndpoint(t, "sys-tok")
		userURL, userForms := recordingTokenEndpoint(t, "user-tok")
		sec := &HTTPToolSecurity{
			Key:          "acme",
			Type:         NullString{Value: "oauth2", Valid: true},
			OAuthTypes:   []string{"client_credentials"},
			OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"https://unused.example/token"}}`),
			EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
		}

		ok, _, _, _ := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{sec},
			map[string]string{"ACME_CLIENT_ID": "sys-id", "ACME_CLIENT_SECRET": "sys-secret", "ACME_TOKEN_URL": sysURL},
			map[string]string{"ACME_CLIENT_SECRET": "user-secret", "ACME_TOKEN_URL": userURL})
		require.True(t, ok)
		require.Empty(t, userForms())
		require.Len(t, sysForms(), 1)
		require.Equal(t, "sys-id", sysForms()[0].Get("client_id"))
	})

	t.Run("saved config client ignores user token URL header", func(t *testing.T) {
		t.Parallel()

		defaultURL, defaultForms := recordingTokenEndpoint(t, "default-tok")
		userURL, userForms := recordingTokenEndpoint(t, "user-tok")
		sec := &HTTPToolSecurity{
			Key:          "acme",
			Type:         NullString{Value: "oauth2", Valid: true},
			OAuthTypes:   []string{"client_credentials"},
			OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"` + defaultURL + `"}}`),
			EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
		}

		report := &toolconfig.CredentialReport{}
		env := toolconfig.ToolCallEnv{
			SystemEnv:   toolconfig.NewCaseInsensitiveEnv(),
			SavedConfig: toolconfig.CIEnvFrom(map[string]string{"ACME_CLIENT_ID": "saved-id", "ACME_CLIENT_SECRET": "saved-secret"}),
			UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_TOKEN_URL": userURL}),
			Credentials: report,
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/resource", nil)
		var status int
		ok := processSecurity(t.Context(), testenv.NewLogger(t), unsafePolicy(t), req, httptest.NewRecorder(), &status, newTestToolDescriptor(), &HTTPToolCallPlan{Security: []*HTTPToolSecurity{sec}}, testenv.NewMemoryCache(), env, "https://api.example", tm.HTTPLogAttributes{})
		require.True(t, ok)
		require.Empty(t, userForms())
		require.Len(t, defaultForms(), 1)
		require.Equal(t, "saved-id", defaultForms()[0].Get("client_id"))
		require.Equal(t, "saved-secret", defaultForms()[0].Get("client_secret"))
		require.Equal(t, []toolconfig.AppliedCredential{{Variable: "ACME_CLIENT_SECRET", System: false, ClientGrant: true, Carrier: "Authorization"}}, report.Sent)
	})

	t.Run("user client with user token URL uses user endpoint", func(t *testing.T) {
		t.Parallel()

		defaultURL, defaultForms := recordingTokenEndpoint(t, "default-tok")
		userURL, userForms := recordingTokenEndpoint(t, "user-tok")
		sec := &HTTPToolSecurity{
			Key:          "acme",
			Type:         NullString{Value: "oauth2", Valid: true},
			OAuthTypes:   []string{"client_credentials"},
			OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"` + defaultURL + `"}}`),
			EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
		}

		ok, _, _, _ := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{sec},
			map[string]string{"ACME_CLIENT_ID": "sys-id", "ACME_CLIENT_SECRET": "sys-secret"},
			map[string]string{"ACME_CLIENT_ID": "user-id", "ACME_CLIENT_SECRET": "user-secret", "ACME_TOKEN_URL": userURL})
		require.True(t, ok)
		require.Empty(t, defaultForms())
		require.Len(t, userForms(), 1)
		require.Equal(t, "user-id", userForms()[0].Get("client_id"))
		require.Equal(t, "user-secret", userForms()[0].Get("client_secret"))
	})
}

func TestServerEnvVarRespectsSavedConfig(t *testing.T) {
	t.Parallel()

	recordingAPI := func(t *testing.T) (string, func() []http.Header) {
		t.Helper()
		var mu sync.Mutex
		var seen []http.Header
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, r.Header.Clone())
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(s.Close)
		return s.URL, func() []http.Header {
			mu.Lock()
			defer mu.Unlock()
			out := make([]http.Header, len(seen))
			copy(out, seen)
			return out
		}
	}

	t.Run("saved config API key blocks user server URL override", func(t *testing.T) {
		t.Parallel()

		defaultAPI, defaultHits := recordingAPI(t)
		userAPI, userHits := recordingAPI(t)
		plan := &HTTPToolCallPlan{
			ServerEnvVar:     "ACME_SERVER_URL",
			DefaultServerUrl: NullString{Value: defaultAPI, Valid: true},
			Security: []*HTTPToolSecurity{{
				Type:         NullString{Value: "apiKey", Valid: true},
				Scheme:       NullString{Value: "apiKey", Valid: true},
				Name:         NullString{Value: "X-API-Key", Valid: true},
				Placement:    NullString{Value: "header", Valid: true},
				EnvVariables: []string{"ACME_API_KEY"},
			}},
			SecurityScopes:     map[string][]string{},
			Method:             http.MethodGet,
			Path:               "/items",
			HeaderParams:       map[string]*HTTPParameter{},
			QueryParams:        map[string]*HTTPParameter{},
			PathParams:         map[string]*HTTPParameter{},
			RequestContentType: NullString{Value: "application/json", Valid: true},
		}

		proxy := NewToolProxy(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), testenv.NewMemoryCache(), unsafePolicy(t), nil, nil, nil)
		err := proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader([]byte(`{}`)), toolconfig.ToolCallEnv{
			SystemEnv:   toolconfig.NewCaseInsensitiveEnv(),
			SavedConfig: toolconfig.CIEnvFrom(map[string]string{"ACME_API_KEY": "saved-api-key"}),
			UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_SERVER_URL": userAPI}),
		}, NewHTTPToolCallPlan(newTestToolDescriptor(), plan), tm.HTTPLogAttributes{})
		require.NoError(t, err)
		require.Empty(t, userHits())
		require.Len(t, defaultHits(), 1)
		require.Equal(t, "saved-api-key", defaultHits()[0].Get("X-API-Key"))
	})

	t.Run("saved config OAuth token blocks user server URL override", func(t *testing.T) {
		t.Parallel()

		defaultAPI, defaultHits := recordingAPI(t)
		userAPI, userHits := recordingAPI(t)
		plan := &HTTPToolCallPlan{
			ServerEnvVar:     "ACME_SERVER_URL",
			DefaultServerUrl: NullString{Value: defaultAPI, Valid: true},
			Security: []*HTTPToolSecurity{{
				Key:          "oauth",
				Type:         NullString{Value: "oauth2", Valid: true},
				OAuthTypes:   []string{"authorization_code"},
				EnvVariables: []string{"ACME_OAUTH_ACCESS_TOKEN"},
			}},
			SecurityScopes:     map[string][]string{},
			Method:             http.MethodGet,
			Path:               "/items",
			HeaderParams:       map[string]*HTTPParameter{},
			QueryParams:        map[string]*HTTPParameter{},
			PathParams:         map[string]*HTTPParameter{},
			RequestContentType: NullString{Value: "application/json", Valid: true},
		}

		proxy := NewToolProxy(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), testenv.NewMemoryCache(), unsafePolicy(t), nil, nil, nil)
		err := proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader([]byte(`{}`)), toolconfig.ToolCallEnv{
			SystemEnv:   toolconfig.NewCaseInsensitiveEnv(),
			SavedConfig: toolconfig.CIEnvFrom(map[string]string{"ACME_OAUTH_ACCESS_TOKEN": "oauth-token"}),
			UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_SERVER_URL": userAPI}),
		}, NewHTTPToolCallPlan(newTestToolDescriptor(), plan), tm.HTTPLogAttributes{})
		require.NoError(t, err)
		require.Empty(t, userHits())
		require.Len(t, defaultHits(), 1)
		require.Equal(t, "Bearer oauth-token", defaultHits()[0].Get("Authorization"))
	})

	t.Run("saved config server URL takes precedence over user server URL", func(t *testing.T) {
		t.Parallel()

		savedAPI, savedHits := recordingAPI(t)
		userAPI, userHits := recordingAPI(t)
		plan := &HTTPToolCallPlan{
			ServerEnvVar:     "ACME_SERVER_URL",
			DefaultServerUrl: NullString{Value: "https://unused.example", Valid: true},
			Security: []*HTTPToolSecurity{{
				Type:         NullString{Value: "apiKey", Valid: true},
				Scheme:       NullString{Value: "apiKey", Valid: true},
				Name:         NullString{Value: "X-API-Key", Valid: true},
				Placement:    NullString{Value: "header", Valid: true},
				EnvVariables: []string{"ACME_API_KEY"},
			}},
			SecurityScopes:     map[string][]string{},
			Method:             http.MethodGet,
			Path:               "/items",
			HeaderParams:       map[string]*HTTPParameter{},
			QueryParams:        map[string]*HTTPParameter{},
			PathParams:         map[string]*HTTPParameter{},
			RequestContentType: NullString{Value: "application/json", Valid: true},
		}

		proxy := NewToolProxy(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), testenv.NewMemoryCache(), unsafePolicy(t), nil, nil, nil)
		err := proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader([]byte(`{}`)), toolconfig.ToolCallEnv{
			SystemEnv:   toolconfig.NewCaseInsensitiveEnv(),
			SavedConfig: toolconfig.CIEnvFrom(map[string]string{"ACME_API_KEY": "saved-api-key", "ACME_SERVER_URL": savedAPI}),
			UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_SERVER_URL": userAPI}),
		}, NewHTTPToolCallPlan(newTestToolDescriptor(), plan), tm.HTTPLogAttributes{})
		require.NoError(t, err)
		require.Empty(t, userHits())
		require.Len(t, savedHits(), 1)
		require.Equal(t, "saved-api-key", savedHits()[0].Get("X-API-Key"))
	})

	t.Run("user server URL works when no configured layers are populated", func(t *testing.T) {
		t.Parallel()

		defaultAPI, defaultHits := recordingAPI(t)
		userAPI, userHits := recordingAPI(t)
		plan := &HTTPToolCallPlan{
			ServerEnvVar:     "ACME_SERVER_URL",
			DefaultServerUrl: NullString{Value: defaultAPI, Valid: true},
			Security: []*HTTPToolSecurity{{
				Type:         NullString{Value: "apiKey", Valid: true},
				Scheme:       NullString{Value: "apiKey", Valid: true},
				Name:         NullString{Value: "X-API-Key", Valid: true},
				Placement:    NullString{Value: "header", Valid: true},
				EnvVariables: []string{"ACME_API_KEY"},
			}},
			SecurityScopes:     map[string][]string{},
			Method:             http.MethodGet,
			Path:               "/items",
			HeaderParams:       map[string]*HTTPParameter{},
			QueryParams:        map[string]*HTTPParameter{},
			PathParams:         map[string]*HTTPParameter{},
			RequestContentType: NullString{Value: "application/json", Valid: true},
		}

		proxy := NewToolProxy(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), testenv.NewMemoryCache(), unsafePolicy(t), nil, nil, nil)
		err := proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader([]byte(`{}`)), toolconfig.ToolCallEnv{
			SystemEnv:   toolconfig.NewCaseInsensitiveEnv(),
			SavedConfig: toolconfig.NewCaseInsensitiveEnv(),
			UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_API_KEY": "user-api-key", "ACME_SERVER_URL": userAPI}),
		}, NewHTTPToolCallPlan(newTestToolDescriptor(), plan), tm.HTTPLogAttributes{})
		require.NoError(t, err)
		require.Empty(t, defaultHits())
		require.Len(t, userHits(), 1)
		require.Equal(t, "user-api-key", userHits()[0].Get("X-API-Key"))
	})
}

// With system environment set, the server URL comes from system environment or the tool itself, as it did before SavedConfig was split out.
func TestServerEnvVarSystemEnvKeepsItsServer(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	hits := map[string][]string{}
	recording := func(name string) string {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits[name] = append(hits[name], r.Header.Get("X-API-Key"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(s.Close)
		return s.URL
	}
	defaultAPI, savedAPI, userAPI := recording("default"), recording("saved"), recording("user")

	plan := &HTTPToolCallPlan{
		ServerEnvVar:     "ACME_SERVER_URL",
		DefaultServerUrl: NullString{Value: defaultAPI, Valid: true},
		Security: []*HTTPToolSecurity{{
			Type:         NullString{Value: "apiKey", Valid: true},
			Scheme:       NullString{Value: "apiKey", Valid: true},
			Name:         NullString{Value: "X-API-Key", Valid: true},
			Placement:    NullString{Value: "header", Valid: true},
			EnvVariables: []string{"ACME_API_KEY"},
		}},
		SecurityScopes:     map[string][]string{},
		Method:             http.MethodGet,
		Path:               "/items",
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
	}

	proxy := NewToolProxy(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), testenv.NewMemoryCache(), unsafePolicy(t), nil, nil, nil)
	err := proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader([]byte(`{}`)), toolconfig.ToolCallEnv{
		SystemEnv:   toolconfig.CIEnvFrom(map[string]string{"ACME_API_KEY": "system-api-key"}),
		SavedConfig: toolconfig.CIEnvFrom(map[string]string{"ACME_SERVER_URL": savedAPI}),
		UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_SERVER_URL": userAPI}),
	}, NewHTTPToolCallPlan(newTestToolDescriptor(), plan), tm.HTTPLogAttributes{})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string][]string{"default": {"system-api-key"}}, hits)
}

// A saved token URL applies to a saved client, and not to a client from system environment.
func TestClientCredentialsSavedTokenURLFollowsClientLayer(t *testing.T) {
	t.Parallel()

	client := map[string]string{"ACME_CLIENT_ID": "client-id", "ACME_CLIENT_SECRET": "client-secret"}

	for _, tc := range []struct {
		name      string
		system    map[string]string
		saved     map[string]string
		wantSaved int
	}{
		{name: "system client", system: client, saved: map[string]string{}, wantSaved: 0},
		{name: "saved client", system: map[string]string{}, saved: client, wantSaved: 1},
		{name: "client split across system and saved", system: map[string]string{"ACME_CLIENT_ID": "client-id"}, saved: map[string]string{"ACME_CLIENT_SECRET": "client-secret"}, wantSaved: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defaultURL, defaultForms := recordingTokenEndpoint(t, "default-tok")
			savedURL, savedForms := recordingTokenEndpoint(t, "saved-tok")
			sec := &HTTPToolSecurity{
				Key:          "acme",
				Type:         NullString{Value: "oauth2", Valid: true},
				OAuthTypes:   []string{"client_credentials"},
				OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"` + defaultURL + `"}}`),
				EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
			}
			saved := toolconfig.CIEnvFrom(tc.saved)
			saved.Set("ACME_TOKEN_URL", savedURL)
			env := toolconfig.ToolCallEnv{
				SystemEnv:   toolconfig.CIEnvFrom(tc.system),
				SavedConfig: saved,
				UserConfig:  toolconfig.NewCaseInsensitiveEnv(),
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/resource", nil)
			var status int
			ok := processSecurity(t.Context(), testenv.NewLogger(t), unsafePolicy(t), req, httptest.NewRecorder(), &status, newTestToolDescriptor(), &HTTPToolCallPlan{Security: []*HTTPToolSecurity{sec}}, testenv.NewMemoryCache(), env, "https://api.example", tm.HTTPLogAttributes{})
			require.True(t, ok)
			require.Len(t, savedForms(), tc.wantSaved)
			require.Len(t, defaultForms(), 1-tc.wantSaved)
		})
	}
}

// A cached token is reused only by a call that holds the client secret that obtained it.
func TestClientCredentialsCachedTokenNeedsSameSecret(t *testing.T) {
	t.Parallel()

	tokenURL, forms := recordingTokenEndpoint(t, "cached-tok")
	sec := &HTTPToolSecurity{
		Key:          "acme",
		Type:         NullString{Value: "oauth2", Valid: true},
		OAuthTypes:   []string{"client_credentials"},
		OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"` + tokenURL + `"}}`),
		EnvVariables: []string{"ACME_CLIENT_ID", "ACME_CLIENT_SECRET", "ACME_TOKEN_URL", "ACME_ACCESS_TOKEN"},
	}
	shared := testenv.NewMemoryCache()
	descriptor := newTestToolDescriptor()
	call := func(env toolconfig.ToolCallEnv) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/resource", nil)
		var status int
		ok := processSecurity(t.Context(), testenv.NewLogger(t), unsafePolicy(t), req, httptest.NewRecorder(), &status, descriptor, &HTTPToolCallPlan{Security: []*HTTPToolSecurity{sec}}, shared, env, "https://api.example", tm.HTTPLogAttributes{})
		require.True(t, ok)
	}
	system := toolconfig.ToolCallEnv{
		SystemEnv:   toolconfig.CIEnvFrom(map[string]string{"ACME_CLIENT_ID": "client-id", "ACME_CLIENT_SECRET": "client-secret"}),
		SavedConfig: toolconfig.NewCaseInsensitiveEnv(),
		UserConfig:  toolconfig.NewCaseInsensitiveEnv(),
	}
	request := toolconfig.ToolCallEnv{
		SystemEnv:   toolconfig.NewCaseInsensitiveEnv(),
		SavedConfig: toolconfig.NewCaseInsensitiveEnv(),
		UserConfig:  toolconfig.CIEnvFrom(map[string]string{"ACME_CLIENT_ID": "client-id", "ACME_CLIENT_SECRET": "another-secret", "ACME_TOKEN_URL": tokenURL}),
	}

	call(system)
	require.Len(t, forms(), 1)

	// The same client and secret reuse the cached token.
	call(system)
	require.Len(t, forms(), 1)

	// The same client ID with another secret asks the token endpoint itself.
	call(request)
	require.Len(t, forms(), 2)
	require.Equal(t, "another-secret", forms()[1].Get("client_secret"))
}
