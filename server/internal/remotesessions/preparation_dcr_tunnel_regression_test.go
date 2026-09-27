package remotesessions

import (
	"encoding/json"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreparationDCRTunneledSubmission(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		state  string
	}{
		{"confirmed", http.StatusCreated, `{"client_id":"tunnel-client","client_secret":"tunnel-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`, "ready"},
		{"redirect", http.StatusTemporaryRedirect, "", "indeterminate"},
		{"oversized", http.StatusCreated, strings.Repeat("x", (1<<20)+1), "indeterminate"},
		{"rejection", http.StatusBadRequest, "", "provider_rejection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var forwards, redirects atomic.Int32
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirects.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(redirect.Close)
			tunnelID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwards.Add(1)
				assert.Equal(t, tunnelID.UUID.String(), r.Header.Get(wire.HeaderTunnelID))
				assert.Equal(t, "forward-token", r.Header.Get(wire.HeaderTunnelForwardToken))
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/register", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body struct {
					GrantTypes []string `json:"grant_types"`
					AuthMethod string   `json:"token_endpoint_auth_method"`
					Scope      string   `json:"scope"`
				}
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, []string{oauthwire.GrantTypeJWTBearer}, body.GrantTypes)
				assert.Equal(t, "client_secret_basic", body.AuthMethod)
				assert.Equal(t, "read", body.Scope)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Location", redirect.URL)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(gateway.Close)
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
			require.NoError(t, err)
			routes := route.NewRouteTable()
			require.NoError(t, routes.Publish(t.Context(), tunnelID.UUID.String(), gateway.URL, time.Minute))
			s := &Service{policy: policy, tunnels: tunnelrouting.NewHTTPClient(routes, "forward-token", policy, nil)}
			// An unresolvable issuer hostname ensures direct egress cannot pass.
			response, state := s.submitPreparationDCRWithTunnel(t.Context(), PreparationInput{Scopes: []string{"read"}}, "https://issuer.invalid/register", "client_secret_basic", tunnelID)
			require.Equal(t, tc.state, state)
			if state == "ready" {
				require.Equal(t, "tunnel-client", response.ClientID)
			}
			require.EqualValues(t, 1, forwards.Load())
			require.Zero(t, redirects.Load(), "registration redirects must not be followed")
		})
	}
}

func TestPreparationDCRTunneledSubmissionFailsClosed(t *testing.T) {
	t.Parallel()
	var direct atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(endpoint.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	for _, tunnels := range []*tunnelrouting.HTTPClient{nil, tunnelrouting.NewHTTPClient(route.NewRouteTable(), "forward-token", policy, nil)} {
		s := &Service{policy: policy, tunnels: tunnels}
		_, state := s.submitPreparationDCRWithTunnel(t.Context(), PreparationInput{}, endpoint.URL, "client_secret_basic", uuid.NullUUID{UUID: uuid.New(), Valid: true})
		require.Equal(t, "indeterminate", state)
	}
	require.Zero(t, direct.Load(), "missing tunnel transport or route must never fall back to direct egress")
}
