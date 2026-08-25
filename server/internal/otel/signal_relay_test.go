package otel

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestSignalRelayCacheReturnsActiveAndRemovesExpiredDestinations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	active, activeTransport := newTrackedRelayDestination()
	expired, expiredTransport := newTrackedRelayDestination()
	relay.destinationCache["active"] = cachedRelayDestination{
		destination: active,
		expiresAt:   now.Add(time.Minute),
	}
	relay.destinationCache["expired"] = cachedRelayDestination{
		destination: expired,
		expiresAt:   now,
	}

	got, ok := relay.cachedDestination("active", now)
	require.True(t, ok)
	require.Same(t, active, got)
	require.Zero(t, activeTransport.closeCalls.Load())

	got, ok = relay.cachedDestination("expired", now)
	require.False(t, ok)
	require.Nil(t, got)
	require.NotContains(t, relay.destinationCache, "expired")
	require.Equal(t, int64(1), expiredTransport.closeCalls.Load())
}

func TestSignalRelayCacheInsertionPrunesExpiredDestinations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	expired, expiredTransport := newTrackedRelayDestination()
	relay.destinationCache["expired"] = cachedRelayDestination{
		destination: expired,
		expiresAt:   now,
	}

	relay.cacheDestination("new", cachedRelayDestination{
		destination: nil,
		expiresAt:   now.Add(time.Minute),
	}, now)

	require.NotContains(t, relay.destinationCache, "expired")
	require.Contains(t, relay.destinationCache, "new")
	require.Equal(t, int64(1), expiredTransport.closeCalls.Load())
}

func TestSignalRelayCacheReplacementClosesOldDestination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	oldDestination, oldTransport := newTrackedRelayDestination()
	newDestination, newTransport := newTrackedRelayDestination()
	relay.cacheDestination("organization-id", cachedRelayDestination{
		destination: oldDestination,
		expiresAt:   now.Add(time.Minute),
	}, now)

	relay.cacheDestination("organization-id", cachedRelayDestination{
		destination: newDestination,
		expiresAt:   now.Add(2 * time.Minute),
	}, now)

	got, ok := relay.cachedDestination("organization-id", now)
	require.True(t, ok)
	require.Same(t, newDestination, got)
	require.Equal(t, int64(1), oldTransport.closeCalls.Load())
	require.Zero(t, newTransport.closeCalls.Load())
}

func TestSignalRelayCacheEvictsEarliestExpiryAtCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	oldestDestination, oldestTransport := newTrackedRelayDestination()
	for i := range relayDestinationCacheMaxEntries {
		destination := (*relayDestination)(nil)
		if i == 0 {
			destination = oldestDestination
		}
		relay.destinationCache[fmt.Sprintf("organization-%04d", i)] = cachedRelayDestination{
			destination: destination,
			expiresAt:   now.Add(time.Duration(i+1) * time.Second),
		}
	}

	relay.cacheDestination("new-organization", cachedRelayDestination{
		destination: nil,
		expiresAt:   now.Add(time.Hour),
	}, now)

	require.Len(t, relay.destinationCache, relayDestinationCacheMaxEntries)
	require.NotContains(t, relay.destinationCache, "organization-0000")
	require.Contains(t, relay.destinationCache, "new-organization")
	require.Equal(t, int64(1), oldestTransport.closeCalls.Load())
}

type closeIdleTrackingRoundTripper struct {
	closeCalls atomic.Int64
}

func (t *closeIdleTrackingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	panic("unexpected HTTP request")
}

func (t *closeIdleTrackingRoundTripper) CloseIdleConnections() {
	t.closeCalls.Add(1)
}

func newTrackedRelayDestination() (*relayDestination, *closeIdleTrackingRoundTripper) {
	transport := new(closeIdleTrackingRoundTripper)
	client := new(http.Client)
	client.Transport = transport
	return &relayDestination{
		organizationID: "",
		endpoint:       "",
		headers:        nil,
		httpClient:     client,
		signalName:     "",
	}, transport
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
