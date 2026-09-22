package mcp

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestFederatedCallbackHTTPS(t *testing.T) {
	t.Parallel()
	for _, callback := range []string{"http://localhost/callback", "http://gram.example/callback", "https:///callback", "https://user:password@gram.example/callback", "https://gram.example/callback#fragment"} {
		_, err := federatedCallbackURL(callback)
		require.ErrorIs(t, err, remotesessions.ErrFederatedConfiguration)
	}
	callback, err := federatedCallbackURL("https://gram.example/mcp/idp_callback")
	require.NoError(t, err)
	require.Equal(t, "https", callback.Scheme)
}

func TestFederatedFailureSafeCauses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		cause error
		code  oops.Code
	}{
		{fmt.Errorf("secret: %w", remotesessions.ErrFederatedConfiguration), oops.CodeFailedPrecondition},
		{fmt.Errorf("secret: %w", remotesessions.ErrFederatedIdentity), oops.CodeUnauthorized},
		{errors.New("secret token response"), oops.CodeUnavailable},
		{fmt.Errorf("secret: %w", remotesessions.ErrFederatedSigning), oops.CodeUnexpected},
		{fmt.Errorf("secret: %w", remotesessions.ErrFederatedUnavailable), oops.CodeUnavailable},
	} {
		code, cause := federatedFailure(test.cause)
		require.Equal(t, test.code, code)
		require.NotContains(t, cause.Error(), "secret")
	}
}

func TestFederatedFailureResponse(t *testing.T) {
	t.Parallel()
	for _, firstParty := range []bool{false, true} {
		for _, declined := range []bool{false, true} {
			t.Run(fmt.Sprintf("firstparty=%t/declined=%t", firstParty, declined), func(t *testing.T) {
				t.Parallel()
				reader := sdkmetric.NewManualReader()
				meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
				t.Cleanup(func() { require.NoError(t, meter.Shutdown(t.Context())) })
				logger := testenv.NewLogger(t)
				serverURL, err := url.Parse("https://gram.example")
				require.NoError(t, err)
				s := &Service{serverURL: serverURL, logger: logger, metrics: mcpmetrics.NewMetrics(meter.Meter("test"), logger)}
				endpoint := &ResolvedMcpEndpoint{RouteBase: "/mcp", Slug: "test-server"}
				state := AuthnChallengeState{FirstParty: firstParty, RedirectURI: "http://127.0.0.1:8080/callback", State: "original-client-state"}
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/idp_callback", nil)
				code := oops.CodeGatewayError
				if declined {
					code = oops.CodeForbidden
				}
				err = s.finishFederatedFailure(w, r, endpoint, state, mcpmetrics.OAuthFlowStageIDPCallback, code, nil, "Safe login failure", declined)
				if firstParty {
					var failure *oops.ShareableError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, code, failure.Code)
					require.Empty(t, w.Header().Get("Location"))
				} else {
					require.NoError(t, err)
					require.Equal(t, http.StatusFound, w.Code)
					target, err := url.Parse(w.Header().Get("Location"))
					require.NoError(t, err)
					require.Equal(t, "http", target.Scheme, "native loopback redirect remains supported")
					require.Equal(t, "original-client-state", target.Query().Get("state"))
					require.Equal(t, "https://gram.example/mcp/test-server", target.Query().Get("iss"))
					require.Equal(t, "Safe login failure", target.Query().Get("error_description"))
					expected := "server_error"
					if declined {
						expected = "access_denied"
					}
					require.Equal(t, expected, target.Query().Get("error"))
				}
				var data metricdata.ResourceMetrics
				require.NoError(t, reader.Collect(t.Context(), &data))
				var terminals int64
				for _, scope := range data.ScopeMetrics {
					for _, metric := range scope.Metrics {
						if metric.Name == "oauth.flow.failed" || metric.Name == "oauth.flow.declined" {
							expectedMetric := "oauth.flow.failed"
							if declined {
								expectedMetric = "oauth.flow.declined"
							}
							require.Equal(t, expectedMetric, metric.Name)
							sum, ok := metric.Data.(metricdata.Sum[int64])
							require.True(t, ok)
							for _, point := range sum.DataPoints {
								terminals += point.Value
							}
						}
					}
				}
				require.EqualValues(t, 1, terminals)
			})
		}
	}
}
