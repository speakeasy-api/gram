package remotesessions

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRegisterDynamicClientDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var redirectedTo atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedTo.Add(1)
	}))
	t.Cleanup(target.Close)

	registration := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, target.URL, http.StatusFound)
	}))
	t.Cleanup(registration.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registration.URL)
	require.NoError(t, err)

	_, err = RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL})
	require.Error(t, err)
	require.Zero(t, redirectedTo.Load(), "DCR must not resend registration data to a redirect target")
}

// The issuer's issuance and expiry stamps ride the registration result in
// both the persisted and the wire forms, and the endpoint the client was
// registered at is echoed, so every create path can record the provenance a
// later rotation needs.
func TestRegisterDynamicClientCarriesRegistrationStamps(t *testing.T) {
	t.Parallel()

	registration := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"secret","token_endpoint_auth_method":"client_secret_basic","client_id_issued_at":1789142562,"client_secret_expires_at":1796918562}`))
	}))
	t.Cleanup(registration.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registration.URL)
	require.NoError(t, err)

	registered, err := RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL + "/register"})
	require.NoError(t, err)

	require.Equal(t, registration.URL+"/register", registered.RegistrationEndpoint)
	require.True(t, registered.ClientIDIssuedAt.Valid)
	require.Equal(t, int64(1789142562), registered.ClientIDIssuedAt.Time.Unix())
	require.True(t, registered.ClientSecretExpiresAt.Valid)
	require.Equal(t, int64(1796918562), registered.ClientSecretExpiresAt.Time.Unix())
	require.Equal(t, "2026-09-11T16:02:42Z", registered.ClientIDIssuedAtRFC3339)
	require.Equal(t, "2026-12-10T16:02:42Z", registered.ClientSecretExpiresAtRFC3339)
}

// RFC 7591 §3.2.1: a client_secret_expires_at of 0 means the secret does not
// expire, and a public client gets no stamps at all. Neither must be rendered
// as a real timestamp.
func TestRegisterDynamicClientOmitsAbsentRegistrationStamps(t *testing.T) {
	t.Parallel()

	registration := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","token_endpoint_auth_method":"none","client_secret_expires_at":0}`))
	}))
	t.Cleanup(registration.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registration.URL)
	require.NoError(t, err)

	registered, err := RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL})
	require.NoError(t, err)

	require.False(t, registered.ClientIDIssuedAt.Valid)
	require.False(t, registered.ClientSecretExpiresAt.Valid)
	require.Empty(t, registered.ClientIDIssuedAtRFC3339)
	require.Empty(t, registered.ClientSecretExpiresAtRFC3339)
}
