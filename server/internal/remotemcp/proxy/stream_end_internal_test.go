package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSSEEventHasID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		nonData string
		want    bool
	}{
		{name: "id with value", nonData: "id: evt-1\n", want: true},
		{name: "id without space", nonData: "id:evt-1\n", want: true},
		{name: "id among other fields", nonData: "event: message\nid: 7\n", want: true},
		{name: "empty id clears the last event id", nonData: "id:\n", want: false},
		{name: "empty id after the stripped space", nonData: "id: \n", want: false},
		{name: "bare id field clears the last event id", nonData: "id\n", want: false},
		{name: "no id field", nonData: "event: message\nretry: 1000\n", want: false},
		{name: "field that only starts with id", nonData: "identity: x\n", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sseEventHasID([]byte(tc.nonData)))
		})
	}
}

func TestStreamEndReasonPrefersCleanEOFOverDrain(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancel(t.Context())
	sr := newStreamReader(io.NopCloser(strings.NewReader("")), time.Hour, cancel)
	defer func() { _ = sr.Close() }()
	// The upstream reaches EOF, and the drain marks the stream before the
	// relay has finished classifying it.
	_, err := sr.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
	sr.drained.Store(true)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	require.Equal(t, StreamEndUpstreamClosed, streamEndReason(req, sr, nil))
	require.Equal(t, StreamEndShutdown, streamEndReason(req, sr, errors.New("read canceled")))
}

func TestUpstreamCancelTarget(t *testing.T) {
	t.Parallel()

	p := &Proxy{RemoteURL: "https://mcp.example.com/mcp"}

	tests := []struct {
		name       string
		answeredBy string
		want       string
	}{
		{name: "no response", answeredBy: "", want: "https://mcp.example.com/mcp"},
		{name: "no redirect", answeredBy: "https://mcp.example.com/mcp", want: "https://mcp.example.com/mcp"},
		{name: "same-origin redirect", answeredBy: "https://mcp.example.com/v2/mcp", want: "https://mcp.example.com/v2/mcp"},
		{name: "cross-origin redirect", answeredBy: "https://other.example.com/mcp", want: "https://mcp.example.com/mcp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var answered *url.URL
			if tc.answeredBy != "" {
				var err error
				answered, err = url.Parse(tc.answeredBy)
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, p.upstreamCancelTarget(answered))
		})
	}
}

func TestStreamEndReasonPrefersShutdownOverRotation(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancel(t.Context())
	sr := newStreamReader(io.NopCloser(strings.NewReader("")), time.Hour, cancel)
	defer func() { _ = sr.Close() }()
	sr.rotate()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	require.Equal(t, StreamEndMaxAge, streamEndReason(req, sr, errors.New("read canceled")))

	// A shutdown that lands as the stream rotates is the more useful label:
	// the drain reaches every stream, the age only this one.
	sr.drained.Store(true)
	require.Equal(t, StreamEndShutdown, streamEndReason(req, sr, errors.New("read canceled")))
}

func TestStreamAge(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)

	disabled := (&Proxy{MaxStreamAge: 0}).streamAge(start)
	require.False(t, disabled.reached(start.Add(24*time.Hour)), "no maximum age is never reached")
	require.NotPanics(t, disabled.afterDeadline(func() { t.Error("a disabled age must not fire") }))

	age := (&Proxy{MaxStreamAge: 55 * time.Minute}).streamAge(start)
	require.False(t, age.reached(start.Add(55*time.Minute-time.Nanosecond)))
	require.True(t, age.reached(start.Add(55*time.Minute)))
}
