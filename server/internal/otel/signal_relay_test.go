package otel

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestSignalRelayDestinationDrainsFailedResponses(t *testing.T) {
	t.Parallel()

	var newConnections atomic.Int64
	errorBody := strings.Repeat("x", maxRelayErrorBodyBytes+1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, errorBody)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	relay := newSignalRelay(nil, nil, policy, "/v1/traces", "trace")
	destination, err := relay.newDestination("organization-id", server.URL, nil)
	require.NoError(t, err)

	require.Error(t, destination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{}))
	require.Error(t, destination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{}))
	require.Equal(t, int64(1), newConnections.Load())
}

func TestSignalRelayDestinationSanitizesResponseDiagnostics(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/permanent":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "line one\nline two\x07")
		case "/retryable":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "provider-secret-marker")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	permanentRelay := newSignalRelay(nil, nil, policy, "/permanent", "trace")
	permanentDestination, err := permanentRelay.newDestination("organization-id", server.URL, nil)
	require.NoError(t, err)

	err = permanentDestination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "line one line two ")
	require.NotContains(t, err.Error(), "\n")
	require.NotContains(t, err.Error(), "\x07")

	retryableRelay := newSignalRelay(nil, nil, policy, "/retryable", "trace")
	retryableDestination, err := retryableRelay.newDestination("organization-id", server.URL, nil)
	require.NoError(t, err)

	err = retryableDestination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "provider-secret-marker")
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
