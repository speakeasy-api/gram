package platformmcp

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestOAuthTelemetryUsesOnlyBoundedAttributes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := NewOAuthTelemetry(testenv.NewLogger(t), provider)

	telemetry.Record(t.Context(), OAuthEvent{Operation: "refresh", Outcome: "invalid_grant", Reason: "refresh_reuse"})
	telemetry.Record(t.Context(), OAuthEvent{Operation: "refresh", Outcome: "invalid_grant", Reason: "https://untrusted.example/token"})
	telemetry.RecordRefreshSuccess(t.Context(), 250*time.Millisecond, 7*24*time.Hour)
	telemetry.RecordTerminalTransition(t.Context(), platformoauth.ReauthorizationReasonRefreshReuse)
	telemetry.RecordTerminalTransition(t.Context(), platformoauth.ReauthorizationReason("token-secret"))

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))

	events := oauthCounterPoints(t, metrics, platformMCPOAuthEventMetric)
	require.Len(t, events, 1)
	require.Equal(t, int64(1), events[0].Value)
	requireMetricAttribute(t, events[0].Attributes, "platform_mcp.operation", "refresh")
	requireMetricAttribute(t, events[0].Attributes, "platform_mcp.outcome", "invalid_grant")
	requireMetricAttribute(t, events[0].Attributes, "platform_mcp.reason", "refresh_reuse")
	for _, kv := range events[0].Attributes.ToSlice() {
		require.NotContains(t, kv.Value.AsString(), "token-secret")
		require.NotContains(t, kv.Value.AsString(), "untrusted.example")
	}

	refreshDuration := oauthHistogramPoints(t, metrics, platformMCPOAuthRefreshDurationMetric)
	require.Len(t, refreshDuration, 1)
	require.Equal(t, uint64(1), refreshDuration[0].Count)
	requireMetricAttribute(t, refreshDuration[0].Attributes, "platform_mcp.operation", "refresh")
	requireMetricAttribute(t, refreshDuration[0].Attributes, "platform_mcp.outcome", "succeeded")

	connectionAge := oauthHistogramPoints(t, metrics, platformMCPOAuthConnectionAgeMetric)
	require.Len(t, connectionAge, 1)
	require.Equal(t, uint64(1), connectionAge[0].Count)

	transitions := oauthCounterPoints(t, metrics, platformMCPOAuthReauthorizationRequiredMetric)
	require.Len(t, transitions, 1)
	require.Equal(t, int64(1), transitions[0].Value)
	requireMetricAttribute(t, transitions[0].Attributes, "platform_mcp.reason", "refresh_reuse")
}

func TestOAuthResponseRecorderDoesNotRetainResponseBody(t *testing.T) {
	t.Parallel()

	recorder := &oauthResponseRecorder{ResponseWriter: testResponseWriter{}}
	_, err := recorder.Write([]byte("access-token refresh-token authorization-code"))

	require.NoError(t, err)
	require.Equal(t, "succeeded", recorder.outcome())
	require.NotContains(t, strings.Join([]string{recorder.oauthOutcome, recorder.reason}, " "), "token")
}

func TestOAuthResponseRecorderClassifiesSuccessfulRedirectExplicitly(t *testing.T) {
	t.Parallel()

	recorder := &oauthResponseRecorder{ResponseWriter: testResponseWriter{}}
	recorder.setOAuthOutcome("succeeded")
	recorder.WriteHeader(http.StatusSeeOther)

	require.Equal(t, "succeeded", recorder.outcome())
}

func TestOAuthTelemetryAcceptsOnlyBoundedDimensions(t *testing.T) {
	t.Parallel()

	require.True(t, validOAuthEvent(OAuthEvent{Operation: "runtime_auth", Outcome: "access_denied", Reason: "authorization_denied"}))
	require.False(t, validOAuthEvent(OAuthEvent{Operation: "https://untrusted.example", Outcome: "succeeded"}))
	require.False(t, validOAuthEvent(OAuthEvent{Operation: "refresh", Outcome: "succeeded", Reason: "access-token"}))
}

func oauthCounterPoints(t *testing.T, resourceMetrics metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()

	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %q is not a counter", name)
			return sum.DataPoints
		}
	}
	t.Fatalf("missing metric %q", name)
	return nil
}

func oauthHistogramPoints(t *testing.T, resourceMetrics metricdata.ResourceMetrics, name string) []metricdata.HistogramDataPoint[float64] {
	t.Helper()

	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "metric %q is not a histogram", name)
			return histogram.DataPoints
		}
	}
	t.Fatalf("missing metric %q", name)
	return nil
}

func requireMetricAttribute(t *testing.T, attributes attribute.Set, key, want string) {
	t.Helper()

	value, ok := attributes.Value(attribute.Key(key))
	require.True(t, ok, "missing attribute %q", key)
	require.Equal(t, want, value.AsString())
}

type testResponseWriter struct{}

func (testResponseWriter) Header() http.Header            { return http.Header{} }
func (testResponseWriter) Write(body []byte) (int, error) { return len(body), nil }
func (testResponseWriter) WriteHeader(int)                {}
