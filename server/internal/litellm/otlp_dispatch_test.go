package litellm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func dispatchMetricRequest(t *testing.T, handler http.Handler, path, contentType, key string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Gram-Key", key)
	req.Header.Set("Gram-Project", "project-test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestOTLPMetricsDispatchRoutesLiteLLMTraffic(t *testing.T) {
	t.Parallel()

	litellmAuth := testAuthContext()
	litellmAuth.APIKeyName = auth.LiteLLMAPIKeyNamePrefix + "test-instance"
	authorizer := &traceTestAuthorizer{authCtx: litellmAuth, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	persisted := make(chan []telemetry.LogParams, 4)
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(_ context.Context, params []telemetry.LogParams) error {
		persisted <- params
		return nil
	})

	harnessHits := 0
	handler := OTLPMetricsDispatch(func() *Service { return service })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		harnessHits++
		w.WriteHeader(http.StatusOK)
	}))

	jsonBody := testenv.ReadFixture(t, contractFixtureDir+"otlp-metrics.json")
	protobufBody := testenv.ReadFixture(t, contractFixtureDir+"otlp-metrics.pb")

	// Protobuf can only come from an OTLP exporter: routed to LiteLLM without
	// consulting the key.
	require.Equal(t, http.StatusAccepted,
		dispatchMetricRequest(t, handler, canonicalOTLPMetricsPath, "application/x-protobuf", "valid-key", protobufBody).Code)
	// JSON with a litellm-named key: routed to LiteLLM.
	require.Equal(t, http.StatusAccepted,
		dispatchMetricRequest(t, handler, canonicalOTLPMetricsPath, "application/json", "valid-key", jsonBody).Code)
	require.Len(t, <-persisted, len(litellmMetricNames))
	require.Len(t, <-persisted, len(litellmMetricNames))
	require.Zero(t, harnessHits)

	// JSON that fails authentication falls through so the harness handler
	// renders its own error.
	require.Equal(t, http.StatusOK,
		dispatchMetricRequest(t, handler, canonicalOTLPMetricsPath, "application/json", "wrong-key", jsonBody).Code)
	require.Equal(t, 1, harnessHits)

	// Other routes and methods pass through untouched.
	require.Equal(t, http.StatusOK,
		dispatchMetricRequest(t, handler, "/rpc/hooks.otel/v1/logs", "application/json", "valid-key", jsonBody).Code)
	require.Equal(t, 2, harnessHits)
}

func TestOTLPMetricsDispatchLeavesHarnessKeysAlone(t *testing.T) {
	t.Parallel()

	authorizer := &traceTestAuthorizer{authCtx: testAuthContext(), key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })

	harnessHits := 0
	handler := OTLPMetricsDispatch(func() *Service { return service })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		harnessHits++
		w.WriteHeader(http.StatusOK)
	}))

	jsonBody := testenv.ReadFixture(t, contractFixtureDir+"otlp-metrics.json")
	require.Equal(t, http.StatusOK,
		dispatchMetricRequest(t, handler, canonicalOTLPMetricsPath, "application/json", "valid-key", jsonBody).Code)
	require.Equal(t, 1, harnessHits)
}
