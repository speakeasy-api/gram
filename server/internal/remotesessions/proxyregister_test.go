package remotesessions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

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

	_, err = RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registration.URL}, nil)
	require.Error(t, err)
	require.Zero(t, redirectedTo.Load(), "DCR must not resend registration data to a redirect target")
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

	_, err = RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
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

	_, err = RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.Error(t, err)
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

		_, err = RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
		require.Error(t, err, status)
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

	_, err = RegisterDynamicClient(ctx, policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, recorder)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, recorder.failures)
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

	first, err := RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, nil)
	require.NoError(t, err)
	second, err := RegisterDynamicClient(t.Context(), policy, serverURL, ProxyRegisterRequest{RegistrationEndpoint: registrationServer.URL}, nil)
	require.NoError(t, err)
	require.Equal(t, first.ClientID, second.ClientID)
}
