package telemetry_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubRoundTripper struct {
	err error
}

func (s stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, s.err
}

// A transport error fires once per attempt, below the gateway's retry layer, so
// it must log at WARN, not ERROR: retried-and-recovered failures (e.g. the Fly
// autostop EOF race) should not flood error dashboards. The gateway logs the
// final, unrecovered failure as an ERROR once retries are exhausted.
func TestToolCallLogRoundTripper_TransportErrorLogsWarn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	rt := tm.NewToolCallLogRoundTripper(
		stubRoundTripper{err: io.EOF},
		logger,
		testenv.NewTracerProvider(t).Tracer("test"),
		tm.ToolInfo{URN: "urn:gram:test"},
		tm.HTTPLogAttributes{},
	)

	req := httptest.NewRequest(http.MethodPost, "https://app.fly.dev/tool-call", http.NoBody)
	resp, err := rt.RoundTrip(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	require.Error(t, err)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, resp)

	var entry struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
	}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	require.Equal(t, "HTTP roundtrip failed", entry.Msg)
	require.Equal(t, slog.LevelWarn.String(), entry.Level, "transport errors must log at WARN so recovered retries do not look like errors")
}
