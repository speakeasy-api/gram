package externalmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Header recovery is diagnostic, not logical usage: each logical CallTool
// permits one recovery attempt, even though it can send two upstream calls.
func TestParameterHeadersRecoveryMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                       string
		mismatch, repeated, absent bool
		invalidSchema, isError     bool
		wantError                  bool
		wantCalls, wantLists       int
		wantMismatch, wantAttempt  int64
		wantSuccess, wantFailure   int64
		wantExhaustion             int64
	}{
		{name: "no_mismatch", wantCalls: 1},
		{name: "no_mismatch_tool_error", isError: true, wantCalls: 1},
		{name: "recovery_success", mismatch: true, wantCalls: 2, wantLists: 1, wantMismatch: 1, wantAttempt: 1, wantSuccess: 1},
		{name: "refresh_invalid", mismatch: true, invalidSchema: true, wantError: true, wantCalls: 1, wantLists: 1, wantMismatch: 1, wantAttempt: 1, wantFailure: 1},
		{name: "refresh_missing", mismatch: true, absent: true, wantError: true, wantCalls: 1, wantLists: 1, wantMismatch: 1, wantAttempt: 1, wantFailure: 1},
		{name: "replay_exhaustion", mismatch: true, repeated: true, wantError: true, wantCalls: 2, wantLists: 1, wantMismatch: 2, wantAttempt: 1, wantFailure: 1, wantExhaustion: 1},
		{name: "replay_tool_error", mismatch: true, isError: true, wantCalls: 2, wantLists: 1, wantMismatch: 1, wantAttempt: 1, wantFailure: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			metrics := NewMetrics(provider, testenv.NewLogger(t))
			s := &parameterServer{schema: annotatedSchema, absent: tc.absent, isError: tc.isError}
			if tc.invalidSchema {
				s.schema = `{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"invalid header"}}}`
			}
			if tc.mismatch {
				s.reject = func(n int) (int, int) {
					if n == 1 || tc.repeated {
						return http.StatusBadRequest, mcp.CodeHeaderMismatch
					}
					return 0, 0
				}
			}
			c := newParameterClient(t, s, &ClientOptions{Metrics: metrics})()
			require.Same(t, metrics, c.options.Metrics)
			member, err := baggage.NewMember("gram.organization.id", "example-org")
			require.NoError(t, err)
			bag, err := baggage.New(member)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(baggage.ContextWithBaggage(t.Context(), bag), 5*time.Second)
			defer cancel()
			result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{"owner":"example-owner","secret":"example-secret"}`), json.RawMessage(annotatedSchema))
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tc.isError, result.IsError)
			}
			require.NoError(t, ctx.Err(), "recovery must terminate without waiting for the deadline")
			require.Len(t, s.matching("tools/call"), tc.wantCalls)
			require.Len(t, s.matching("tools/list"), tc.wantLists)
			require.Len(t, s.matching("server/discover"), 1+int(tc.wantAttempt))
			assertParameterRecoveryCounters(t, reader, map[string]int64{
				"externalmcp.header.mismatches":           tc.wantMismatch,
				"externalmcp.header.recovery.attempts":    tc.wantAttempt,
				"externalmcp.header.recovery.successes":   tc.wantSuccess,
				"externalmcp.header.recovery.failures":    tc.wantFailure,
				"externalmcp.header.recovery.exhaustions": tc.wantExhaustion,
			})
		})
	}
}

func assertParameterRecoveryCounters(t *testing.T, reader *sdkmetric.ManualReader, expected map[string]int64) {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	actual := make(map[string]int64, len(expected))
	for name := range expected {
		actual[name] = 0 // OTel omits instruments that have never recorded.
	}
	seen := make(map[string]bool)
	for _, scope := range data.ScopeMetrics {
		require.Equal(t, "github.com/speakeasy-api/gram/server/internal/externalmcp", scope.Scope.Name)
		for _, instrument := range scope.Metrics {
			_, known := expected[instrument.Name]
			require.True(t, known, "unexpected metric %q", instrument.Name)
			require.False(t, seen[instrument.Name], "duplicate instrument %q", instrument.Name)
			seen[instrument.Name] = true
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.True(t, sum.IsMonotonic)
			require.Equal(t, metricdata.CumulativeTemporality, sum.Temporality)
			require.Len(t, sum.DataPoints, 1)
			require.Zero(t, sum.DataPoints[0].Attributes.Len(), "request values must not create dimensions")
			actual[instrument.Name] = sum.DataPoints[0].Value
		}
	}
	require.Equal(t, expected, actual)
}
