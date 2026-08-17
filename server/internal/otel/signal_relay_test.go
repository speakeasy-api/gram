package otel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

func TestSignalRelayDestinationUsesConfiguredSignalEndpoint(t *testing.T) {
	t.Parallel()

	requestPath := ""
	requestContentType := ""
	requestHeader := ""
	var requestReadErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestPath = request.URL.Path
		requestContentType = request.Header.Get("Content-Type")
		requestHeader = request.Header.Get("Authorization")
		_, requestReadErr = io.Copy(io.Discard, request.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	relay := newSignalRelay(nil, nil, policy, "/v1/metrics", "metric")
	destination, err := relay.newDestination("organization-id", server.URL, map[string]string{
		"Authorization": "Bearer test-token",
	})
	require.NoError(t, err)

	err = destination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{})

	require.NoError(t, err)
	require.NoError(t, requestReadErr)
	require.Equal(t, "/v1/metrics", requestPath)
	require.Equal(t, "application/x-protobuf", requestContentType)
	require.Equal(t, "Bearer test-token", requestHeader)
}

func TestClassifyRelayStatusDistinguishesRetryableFailures(t *testing.T) {
	t.Parallel()

	reason, retryable := classifyRelayStatus(http.StatusServiceUnavailable)
	require.Equal(t, relayReasonHTTP5xx, reason)
	require.True(t, retryable)

	reason, retryable = classifyRelayStatus(http.StatusRequestTimeout)
	require.Equal(t, relayReasonHTTP4xx, reason)
	require.True(t, retryable)

	reason, retryable = classifyRelayStatus(http.StatusTooManyRequests)
	require.Equal(t, relayReasonHTTP4xx, reason)
	require.True(t, retryable)

	reason, retryable = classifyRelayStatus(http.StatusUnauthorized)
	require.Equal(t, relayReasonPermanentHTTPError, reason)
	require.False(t, retryable)
}
