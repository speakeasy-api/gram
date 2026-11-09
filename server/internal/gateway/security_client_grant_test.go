package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

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

	for _, tc := range []struct {
		name     string
		tokenURL string
		blocking bool
		want     toolconfig.ClientGrantFailure
	}{
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
				policy = unsafePolicy(t)
			}
			ok, status, w, report := runSecurity(t, policy, []*HTTPToolSecurity{machineSecurity},
				map[string]string{"MACHINE_CLIENT_ID": "id", "MACHINE_CLIENT_SECRET": "secret", "MACHINE_TOKEN_URL": tc.tokenURL}, nil)
			require.False(t, ok)
			require.Equal(t, http.StatusUnauthorized, status)
			require.Equal(t, tc.want, report.GrantFailure)
			require.Equal(t, []toolconfig.AppliedCredential{{Variable: "MACHINE_CLIENT_SECRET", System: true, ClientGrant: true}}, report.Sent)
			require.Equal(t, []string{"Content-Type"}, keys(w.Header()))
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
			require.Equal(t, []toolconfig.AppliedCredential{{Variable: "MACHINE_CLIENT_SECRET", System: true, ClientGrant: true}}, report.Sent)

			system["AUTHORIZATION"] = "Bearer saved"
			ok, _, _, report = runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{machineSecurity}, system, nil)
			require.True(t, ok)
			require.Empty(t, report.GrantFailure)
			require.Equal(t, []toolconfig.AppliedCredential{{Variable: "authorization", System: true, ClientGrant: false}}, report.Sent)
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
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "MACHINE_ACCESS_TOKEN", System: false, ClientGrant: false}}, report.Sent)
}

// A scheme's Authorization wins over a saved system Authorization, which is then not reported.
func TestSchemeAuthorizationHidesSystemAuthorization(t *testing.T) {
	t.Parallel()

	bearer := &HTTPToolSecurity{Key: "bearer", Type: NullString{Value: "http", Valid: true}, Scheme: NullString{Value: "bearer", Valid: true}, EnvVariables: []string{"CALLER_TOKEN"}}
	ok, _, _, report := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{bearer},
		map[string]string{"AUTHORIZATION": "Bearer saved"}, map[string]string{"CALLER_TOKEN": "caller"})
	require.True(t, ok)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "CALLER_TOKEN", System: false, ClientGrant: false}}, report.Sent)

	ok, _, _, report = runSecurity(t, unsafePolicy(t), nil, map[string]string{"AUTHORIZATION": "Bearer saved"}, nil)
	require.True(t, ok)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "authorization", System: true, ClientGrant: false}}, report.Sent)
}

// A value missing from the caller's layer is reported as the system one that was sent.
func TestSystemFallbackIsReportedAsSystem(t *testing.T) {
	t.Parallel()

	apiKey := &HTTPToolSecurity{Key: "api_key", Type: NullString{Value: "apiKey", Valid: true}, Name: NullString{Value: "X-Api-Key", Valid: true}, Placement: NullString{Value: "header", Valid: true}, EnvVariables: []string{"API_KEY"}}
	ok, _, _, report := runSecurity(t, unsafePolicy(t), []*HTTPToolSecurity{apiKey}, map[string]string{"API_KEY": "saved"}, nil)
	require.True(t, ok)
	require.Equal(t, []toolconfig.AppliedCredential{{Variable: "API_KEY", System: true, ClientGrant: false}}, report.Sent)
}

func keys(header http.Header) []string {
	out := make([]string, 0, len(header))
	for k := range header {
		out = append(out, k)
	}
	return out
}
