package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
			require.Equal(t, []string{"Content-Type"}, keys(w.Header()))
			require.JSONEq(t, `{"error":`+strconv.Quote(clientGrantFailureMessage(tc.want))+`}`, w.Body.String())
			switch tc.want {
			case toolconfig.ClientGrantRejected:
				require.Contains(t, w.Body.String(), "its token service refused the request. Check the tool's client ID, client secret, scopes and token URL. If you did not set this tool up, ask your administrator.")
			case toolconfig.ClientGrantMisconfigured:
				require.Contains(t, w.Body.String(), "its token service gave an answer Speakeasy could not use. Check the tool's token URL. If you did not set this tool up, ask your administrator.")
			case toolconfig.ClientGrantUnavailable, toolconfig.ClientGrantBlocked, toolconfig.ClientGrantAddressNotFound:
			}
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
