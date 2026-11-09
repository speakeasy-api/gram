package remotemcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A client retrying initialize in a loop folds into a few rows that still count every failure.
func TestRequestConnections_RepeatedRejectionsFold(t *testing.T) {
	t.Parallel()

	// More than the limiter's per-key burst, so some must fold.
	const attempts = 12

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, mcpconnectionattemptstest.LogsOn)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(cleanupCtx)
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	serverID := uuid.New()
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	p := &proxy.Proxy{
		GuardianPolicy:       policy,
		Logger:               testenv.NewLogger(t),
		Tracer:               tracerProvider.Tracer("request-connections-test"),
		NonStreamingTimeout:  5 * time.Second,
		StreamingTimeout:     5 * time.Second,
		MaxBufferedBodyBytes: proxy.DefaultMaxBufferedBodyBytes,
		RemoteURL:            upstream.URL,
		RequestFailureInterceptors: remotemcp.RequestFailureConnections(remotemcp.ConnectionAttribution{
			Scope:            mcpconnectionattempts.ServerScope(uuid.NewString(), uuid.New(), serverID, mcpconnectionattempts.AppUnverified),
			Details:          nil,
			UpstreamFailures: true,
			SelfCredential:   false,
			Request:          nil,
		}, recorder, "https://provider.example.com/mcp", "token"),
	}

	for range attempts {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rr := httptest.NewRecorder()
		require.NoError(t, p.Post(rr, req))
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	var rows []struct {
		Reason  string `ch:"reason"`
		Repeats uint32 `ch:"repeats"`
	}
	require.NoError(t, conn.Select(t.Context(), &rows, `SELECT reason, repeats FROM mcp_connection_events WHERE server_id = ?`, serverID.String()))
	require.NotEmpty(t, rows)
	require.Less(t, len(rows), attempts, "repeats fold instead of writing a row each")
	total := 0
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.CodeProviderRejected.String(), row.Reason)
		total += 1 + int(row.Repeats)
	}
	require.Equal(t, attempts, total, "folded rows still count every failure")
}
