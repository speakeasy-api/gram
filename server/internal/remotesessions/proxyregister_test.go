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

	_, err = RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL})
	require.Error(t, err)
	require.Zero(t, redirectedTo.Load(), "DCR must not resend registration data to a redirect target")
}
