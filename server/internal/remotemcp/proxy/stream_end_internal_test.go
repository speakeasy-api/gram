package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	// The drain marked the stream after the upstream had already reached EOF.
	sr.drained.Store(true)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	require.Equal(t, StreamEndUpstreamClosed, streamEndReason(req, sr, nil))
	require.Equal(t, StreamEndShutdown, streamEndReason(req, sr, errors.New("read canceled")))
}
