package otelforwarding

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newCapturingForwarder builds a Forwarder whose logs are written as JSON to
// buf so tests can assert on the emitted attributes. NewUnsafePolicy with no
// disallowed blocks lets the pooled client reach the loopback httptest server.
func newCapturingForwarder(t *testing.T, buf *bytes.Buffer) *Forwarder {
	t.Helper()

	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	return NewForwarder(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), policy)
}

// findLog returns the first captured JSON log record with the given message.
func findLog(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()

	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record))
		if record["msg"] == msg {
			return record
		}
	}

	t.Fatalf("no log record with msg %q in: %s", msg, buf.String())
	return nil
}

func TestForwarderSendErrorStatusLogsURLAndBody(t *testing.T) {
	t.Parallel()

	const downstreamMessage = "forbidden: token expired"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(downstreamMessage))
	}))
	t.Cleanup(server.Close)

	var buf bytes.Buffer
	f := newCapturingForwarder(t, &buf)

	const secretHeaderValue = "Bearer super-secret-token"
	const destURL = "/v1/traces"
	f.send(t.Context(), Job{
		OrgID:       "org-123",
		URL:         server.URL + destURL,
		ContentType: "application/x-protobuf",
		Headers:     map[string]string{"Authorization": secretHeaderValue},
		Body:        []byte("payload"),
	})

	record := findLog(t, &buf, "otel forward returned error status")
	require.EqualValues(t, http.StatusForbidden, record["http.response.status_code"])
	require.Equal(t, "org-123", record["gram.org.id"])
	require.Equal(t, server.URL+destURL, record["url.full"], "destination URL should be logged")
	require.Equal(t, downstreamMessage, record["http.response.body"], "downstream rejection reason should be captured")
	require.NotContains(t, buf.String(), secretHeaderValue, "auth header value must never be logged")
}

func TestForwarderSendRequestFailedLogsURL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newCapturingForwarder(t, &buf)

	// Closed listener: the request transport fails before any response.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()

	f.send(t.Context(), Job{
		OrgID:       "org-456",
		URL:         server.URL + "/v1/metrics",
		ContentType: "application/x-protobuf",
		Headers:     nil,
		Body:        []byte("payload"),
	})

	record := findLog(t, &buf, "otel forward request failed")
	require.Equal(t, "org-456", record["gram.org.id"])
	require.Equal(t, server.URL+"/v1/metrics", record["url.full"])
}

func TestForwarderSendSuccessNoWarning(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	var buf bytes.Buffer
	f := newCapturingForwarder(t, &buf)

	f.send(t.Context(), Job{
		OrgID:       "org-789",
		URL:         server.URL + "/v1/traces",
		ContentType: "application/x-protobuf",
		Headers:     nil,
		Body:        []byte("payload"),
	})

	require.NotContains(t, buf.String(), "otel forward", "successful forward should not emit a warning")
}
