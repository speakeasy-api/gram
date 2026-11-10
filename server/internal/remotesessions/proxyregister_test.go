package remotesessions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureRegistrationFailures struct {
	methods  []registration.Method
	failures []registration.Failure
}

func (c *captureRegistrationFailures) RecordFailure(_ context.Context, method registration.Method, failure registration.Failure) {
	c.methods = append(c.methods, method)
	c.failures = append(c.failures, failure)
}

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

	_, err = RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL}, nil)
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

	registered, err := RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL + "/register"}, nil)
	require.NoError(t, err)

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

	registered, err := RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL}, nil)
	require.NoError(t, err)

	require.False(t, registered.ClientIDIssuedAt.Valid)
	require.False(t, registered.ClientSecretExpiresAt.Valid)
	require.Empty(t, registered.ClientIDIssuedAtRFC3339)
	require.Empty(t, registered.ClientSecretExpiresAtRFC3339)
}

// A registration response carries a client secret, so the endpoint must be
// HTTPS or loopback. Plain HTTP to any other host is refused before any
// request is made, whether the endpoint came from a dashboard form or from an
// issuer row the rotator re-registers at.
func TestRegisterDynamicClient_RefusesPlaintextNonLoopbackEndpoint(t *testing.T) {
	t.Parallel()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.example.com")
	require.NoError(t, err)

	for _, endpoint := range []string{
		"http://idp.example.com/register",
		"http://10.0.0.5/register",
		"ftp://idp.example.com/register",
		"/register",
	} {
		_, err := RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: endpoint}, nil)
		require.ErrorIs(t, err, ErrInvalidDynamicClientRegistrationEndpoint, endpoint)
	}
}

func TestRegisterDynamicClientClassifiesProviderRejection(t *testing.T) {
	t.Parallel()

	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client_metadata","error_description":" bad\nmetadata "}`))
	}))
	t.Cleanup(registrationServer.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registrationServer.URL)
	require.NoError(t, err)
	recorder := &captureRegistrationFailures{}

	_, err = RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.Error(t, err)
	require.Equal(t, []registration.Method{registration.MethodDCR}, recorder.methods)
	require.Len(t, recorder.failures, 1)
	failure := recorder.failures[0]
	require.Equal(t, registration.OutcomeRefused, failure.Outcome)
	require.Equal(t, registration.ReasonAuthorizationRejected, failure.Reason)
	require.False(t, failure.Retryable)
	require.Equal(t, http.StatusBadRequest, *failure.HTTPStatus)
	require.Equal(t, "invalid_client_metadata: bad metadata", *failure.ProviderMessage)
}

func TestRegisterDynamicClientClassifiesUnusableSuccess(t *testing.T) {
	t.Parallel()

	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_secret":"secret-without-client-id"}`))
	}))
	t.Cleanup(registrationServer.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registrationServer.URL)
	require.NoError(t, err)
	recorder := &captureRegistrationFailures{}

	_, err = RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.Error(t, err)
	require.Equal(t, []registration.Method{registration.MethodDCR}, recorder.methods)
	require.Len(t, recorder.failures, 1)
	require.Equal(t, registration.InvalidSuccessResponse(http.StatusCreated), recorder.failures[0])
}

func TestRegisterDynamicClientClassifiesUnsupportedSuccessStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusAccepted, http.StatusNoContent} {
		registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		t.Cleanup(registrationServer.Close)
		policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
		require.NoError(t, err)
		serverURL, err := url.Parse(registrationServer.URL)
		require.NoError(t, err)
		recorder := &captureRegistrationFailures{}

		_, err = RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
		require.Error(t, err, status)
		require.Equal(t, []registration.Method{registration.MethodDCR}, recorder.methods, status)
		require.Len(t, recorder.failures, 1, status)
		require.Equal(t, registration.InvalidSuccessResponse(status), recorder.failures[0], status)
	}
}

func TestRegisterDynamicClientDoesNotRecordCallerCancellation(t *testing.T) {
	t.Parallel()

	registrationServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(registrationServer.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registrationServer.URL)
	require.NoError(t, err)
	recorder := &captureRegistrationFailures{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = RegisterDynamicClient(ctx, policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, recorder.failures)
}

// A caller that gives up is not the provider timing out. Our own 30s budget
// expiring while the caller waits still is, so the guard turns on the caller's
// context rather than on the error alone.
func TestRegisterDynamicClientDoesNotRecordCallerDeadline(t *testing.T) {
	t.Parallel()

	registrationServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(registrationServer.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registrationServer.URL)
	require.NoError(t, err)
	recorder := &captureRegistrationFailures{}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)

	_, err = RegisterDynamicClient(ctx, policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, recorder.failures, "the caller's deadline is not an upstream timeout")
}

func TestProxyRegistrationErrorPreservesOrdinaryProviderRefusals(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict, http.StatusUnprocessableEntity} {
		mapped := proxyRegistrationError(&registration.HTTPError{StatusCode: status, ProviderMessage: "invalid client metadata"})
		require.Equal(t, oops.CodeBadRequest, mapped.Code, status)
		require.Contains(t, mapped.Error(), "identity provider rejected", status)
		require.Contains(t, mapped.Error(), "invalid client metadata", status)
	}
}

func TestProxyRegistrationErrorKeepsRetryableFailuresAsGatewayErrors(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		mapped := proxyRegistrationError(&registration.HTTPError{StatusCode: status, ProviderMessage: "try later"})
		require.Equal(t, oops.CodeGatewayError, mapped.Code, status)
		require.NotContains(t, mapped.Error(), "rejected", status)
	}
}

func TestRegisterDynamicClientAcceptsDuplicateClientIDs(t *testing.T) {
	t.Parallel()

	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"same-client-id","client_secret":"same-secret"}`))
	}))
	t.Cleanup(registrationServer.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(registrationServer.URL)
	require.NoError(t, err)

	recorder := &captureRegistrationFailures{}

	first, err := RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.NoError(t, err)
	second, err := RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.NoError(t, err)
	require.Equal(t, "same-client-id", first.ClientID)
	require.Equal(t, first.ClientID, second.ClientID)
	require.Empty(t, recorder.failures, "a provider reissuing one client_id is not a registration failure")
}

func TestRegisterDynamicClientCanonicalRedirectURI(t *testing.T) {
	t.Parallel()

	const callback = "https://gram.example.com/mcp/remote_login_callback"
	registration := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request DCRRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Model an upstream that allowlists only the canonical callback.
		if len(request.RedirectURIs) != 1 || request.RedirectURIs[0] != callback {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"canonical-client"}`))
	}))
	t.Cleanup(registration.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.example.com")
	require.NoError(t, err)

	registered, err := RegisterDynamicClient(t.Context(), policy, nil, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL}, nil)
	require.NoError(t, err)
	require.Equal(t, "canonical-client", registered.ClientID)
}
