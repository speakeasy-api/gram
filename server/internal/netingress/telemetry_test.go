package netingress

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestTelemetryClampsDimensionsAndExcludesSensitiveValues(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := NewTelemetry(testenv.NewLogger(t), provider)

	telemetry.Record(
		t.Context(),
		"https://customer.invalid/operation",
		"secret-result",
		"postgres://credential@host/database",
		"customer-provider-token",
		250*time.Millisecond,
	)

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	points := networkIngressCounterPoints(t, resourceMetrics, NetworkIngressOperationsMetric)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
	require.Equal(t, attribute.NewSet(
		attr.NetworkIngressOperation(networkingress.OperationUnknown),
		attr.NetworkIngressResult(networkingress.ResultUnknown),
		attr.NetworkIngressReason(networkingress.ReasonUnknown),
		attr.Provider("unknown"),
		attr.NetworkSurface("private"),
	), points[0].Attributes)

	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			require.NotContains(t, candidate.Name, "credential")
		}
	}
}

func TestAttestationTelemetryClassifiesUnavailableVerifierAsError(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := NewTelemetry(testenv.NewLogger(t), provider)
	verifier := NewAttestationVerifier(nil, nil, DefaultTokenAudience, time.Second, telemetry)

	_, err := verifier.Verify(t.Context(), "opaque-token", "192.0.2.1:1234")
	require.Error(t, err)

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	points := networkIngressCounterPoints(t, resourceMetrics, NetworkIngressOperationsMetric)
	require.Len(t, points, 1)
	require.Equal(t, attribute.NewSet(
		attr.NetworkIngressOperation(OperationAttestation),
		attr.NetworkIngressResult(ResultError),
		attr.NetworkIngressReason(ReasonVerifierUnavailable),
		attr.Provider("unknown"),
		attr.NetworkSurface("private"),
	), points[0].Attributes)
}

func TestAttestationTelemetryRetainsCachedProviderOnRecheckFailure(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := NewTelemetry(testenv.NewLogger(t), provider)
	now := time.Now().UTC()
	token := unsignedToken(t, now.Add(time.Minute))
	lookup := &fakeAttestorLookup{ingress: Ingress{Provider: ProviderTailscale}, recheckErr: errors.New("database unavailable")}
	verifier := NewAttestationVerifier(&fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")}, lookup, DefaultTokenAudience, time.Minute, telemetry)
	verifier.now = func() time.Time { return now }
	_, err := verifier.Verify(t.Context(), token, "192.0.2.1:1234")
	require.NoError(t, err)
	verifier.now = func() time.Time { return now.Add(servingStateRecheckTTL) }
	_, err = verifier.Verify(t.Context(), token, "192.0.2.1:1234")
	require.ErrorContains(t, err, "database unavailable")

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	points := networkIngressCounterPoints(t, resourceMetrics, NetworkIngressOperationsMetric)
	require.Len(t, points, 2)
	providerValue, ok := points[1].Attributes.Value(attr.ProviderKey)
	require.True(t, ok)
	require.Equal(t, attr.Provider(ProviderTailscale).Value, providerValue)
}

func TestAttestorTelemetryRecordsHostMismatch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := NewTelemetry(testenv.NewLogger(t), provider)
	handler, err := NewAttestorHandler(AttestorConfig{
		Upstream:     &url.URL{Scheme: "https", Host: "upstream.invalid"},
		ExpectedHost: "private.example.ts.net",
		TokenPath:    "/unused-on-host-mismatch",
		Transport:    http.DefaultTransport,
		Logger:       testenv.NewLogger(t),
		Telemetry:    telemetry,
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "https://other.example.ts.net/mcp/server", nil)
	request.Host = "other.example.ts.net"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNotFound, response.Code)

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	points := networkIngressCounterPoints(t, resourceMetrics, NetworkIngressOperationsMetric)
	require.Len(t, points, 1)
	require.Equal(t, attribute.NewSet(
		attr.NetworkIngressOperation(OperationProxy),
		attr.NetworkIngressResult(ResultDenied),
		attr.NetworkIngressReason(ReasonHostMismatch),
		attr.Provider(ProviderTailscale),
		attr.NetworkSurface("private"),
	), points[0].Attributes)
}

func TestTelemetryNilSafe(t *testing.T) {
	t.Parallel()

	var telemetry *Telemetry
	telemetry.Record(t.Context(), OperationAdmission, ResultAllowed, ReasonNone, ProviderTailscale, time.Millisecond)
	require.NotNil(t, NewTelemetry(testenv.NewLogger(t), nil))
}

func networkIngressCounterPoints(t *testing.T, metrics metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			return sum.DataPoints
		}
	}
	t.Fatalf("metric %s not found", name)
	return nil
}
