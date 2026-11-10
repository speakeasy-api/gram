package chat

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/httpstream"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// noContextWindow leaves completion events unmodified.
func noContextWindow() int { return 0 }

func TestStreamCompletionReportsCompletedAfterDone(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	upstream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	rr := httptest.NewRecorder()

	reason, err := s.streamCompletion(t.Context(), rr, strings.NewReader(upstream), noContextWindow)

	require.NoError(t, err)
	require.Equal(t, httpstream.EndCompleted, reason)
	require.Equal(t, upstream, rr.Body.String(), "events are relayed unchanged")
}

func TestStreamCompletionReportsATruncatedStreamAsUpstreamClosed(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	upstream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	rr := httptest.NewRecorder()

	reason, err := s.streamCompletion(t.Context(), rr, strings.NewReader(upstream), noContextWindow)

	require.NoError(t, err, "the client already has the response headers; only the metric changes")
	require.Equal(t, httpstream.EndUpstreamClosed, reason)
	require.Equal(t, upstream, rr.Body.String())
}
