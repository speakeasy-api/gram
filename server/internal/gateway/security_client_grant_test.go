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

// The 401 the proxy writes for a failed token request says why it failed.
func TestClientGrantFailureMarksResponse(t *testing.T) {
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
		want     ClientGrantFailure
	}{
		{name: "refused", tokenURL: answering(http.StatusUnauthorized), want: ClientGrantRejected},
		{name: "server error", tokenURL: answering(http.StatusInternalServerError), want: ClientGrantUnavailable},
		{name: "down", tokenURL: down.URL, want: ClientGrantUnavailable},
		{name: "wrong url", tokenURL: answering(http.StatusNotFound), want: ClientGrantMisconfigured},
		{name: "blocked address", tokenURL: answering(http.StatusOK), blocking: true, want: ClientGrantBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tracerProvider := testenv.NewTracerProvider(t)
			policy := guardian.NewDefaultPolicy(tracerProvider)
			if !tc.blocking {
				var err error
				policy, err = guardian.NewUnsafePolicy(tracerProvider, []string{})
				require.NoError(t, err)
			}
			security := &HTTPToolSecurity{
				Key:          "machine",
				Type:         NullString{Value: "oauth2", Valid: true},
				OAuthTypes:   []string{"client_credentials"},
				OAuthFlows:   []byte(`{"clientCredentials":{"tokenUrl":"https://unused.example/token"}}`),
				EnvVariables: []string{"MACHINE_CLIENT_ID", "MACHINE_CLIENT_SECRET", "MACHINE_TOKEN_URL"},
			}
			env := toolconfig.ToolCallEnv{SystemEnv: toolconfig.NewCaseInsensitiveEnv(), UserConfig: toolconfig.NewCaseInsensitiveEnv()}
			env.SystemEnv.Set("MACHINE_CLIENT_ID", "id")
			env.SystemEnv.Set("MACHINE_CLIENT_SECRET", "secret")
			env.SystemEnv.Set("MACHINE_TOKEN_URL", tc.tokenURL)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/resource", nil)
			w := httptest.NewRecorder()
			var status int

			ok := processSecurity(t.Context(), testenv.NewLogger(t), policy, req, w, &status, newTestToolDescriptor(), &HTTPToolCallPlan{Security: []*HTTPToolSecurity{security}}, testenv.NewMemoryCache(), env, "https://api.example", tm.HTTPLogAttributes{})
			require.False(t, ok)
			require.Equal(t, http.StatusUnauthorized, status)
			require.Equal(t, string(tc.want), w.Header().Get(ClientGrantFailureHeader))
		})
	}
}
