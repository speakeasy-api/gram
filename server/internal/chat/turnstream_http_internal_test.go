package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/httpstream"
)

// turnStreamTestTimeout bounds every wait in these tests. The loop reacts in
// microseconds, so reaching it means it stopped selecting.
const turnStreamTestTimeout = 5 * time.Second

func runStreamTurnFrames(ctx context.Context, w http.ResponseWriter, frames <-chan TurnFrame, heartbeat <-chan time.Time, maxAge <-chan time.Time) <-chan httpstream.EndReason {
	done := make(chan httpstream.EndReason, 1)
	go func() { done <- streamTurnFrames(ctx, w, frames, heartbeat, maxAge) }()
	return done
}

func waitForReason(t *testing.T, done <-chan httpstream.EndReason) httpstream.EndReason {
	t.Helper()

	select {
	case reason := <-done:
		return reason
	case <-time.After(turnStreamTestTimeout):
		t.Fatal("timed out waiting for the turn stream loop to return")
		return ""
	}
}

func TestStreamTurnFramesEndsAtMaxAgeWithRetry(t *testing.T) {
	t.Parallel()

	frames := make(chan TurnFrame)
	maxAge := make(chan time.Time, 1)
	rr := httptest.NewRecorder()

	done := runStreamTurnFrames(t.Context(), rr, frames, nil, maxAge)
	maxAge <- time.Now()

	require.Equal(t, httpstream.EndMaxAge, waitForReason(t, done))
	require.True(t, strings.HasPrefix(rr.Body.String(), "retry: "), "the stream must end with an SSE retry field: %q", rr.Body.String())
	require.True(t, strings.HasSuffix(rr.Body.String(), "\n\n"))
}

// signallingRecorder closes written on its first write, so a test can order a
// frame after a heartbeat instead of racing the loop's select.
type signallingRecorder struct {
	*httptest.ResponseRecorder
	written chan struct{}
	once    sync.Once
}

func (r *signallingRecorder) Write(p []byte) (int, error) {
	r.once.Do(func() { close(r.written) })
	n, err := r.ResponseRecorder.Write(p)
	if err != nil {
		return n, fmt.Errorf("record write: %w", err)
	}
	return n, nil
}

func TestStreamTurnFramesWritesHeartbeats(t *testing.T) {
	t.Parallel()

	frames := make(chan TurnFrame)
	heartbeat := make(chan time.Time, 1)
	rr := &signallingRecorder{ResponseRecorder: httptest.NewRecorder(), written: make(chan struct{}), once: sync.Once{}}

	done := runStreamTurnFrames(t.Context(), rr, frames, heartbeat, nil)
	heartbeat <- time.Now()
	select {
	case <-rr.written:
	case <-time.After(turnStreamTestTimeout):
		t.Fatal("timed out waiting for the heartbeat to be written")
	}
	frames <- TurnFrame{Kind: TurnFrameDone, Cursor: "1-0", Text: "", MessageID: "", ToolCalls: nil, FinishReason: "stop", ToolCallID: "", Output: nil}

	require.Equal(t, httpstream.EndCompleted, waitForReason(t, done))
	require.Equal(t, ": keepalive\n\ndata: {\"kind\":\"done\",\"cursor\":\"1-0\",\"finish_reason\":\"stop\"}\n\n", rr.Body.String())
}

func TestStreamTurnFramesEndsWhenClientGoesAway(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	frames := make(chan TurnFrame)
	rr := httptest.NewRecorder()

	done := runStreamTurnFrames(ctx, rr, frames, nil, nil)
	cancel()

	require.Equal(t, httpstream.EndClientClosed, waitForReason(t, done))
	require.Empty(t, rr.Body.String())
}

func TestStreamTurnFramesReportsServerShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	frames := make(chan TurnFrame)
	rr := httptest.NewRecorder()

	done := runStreamTurnFrames(ctx, rr, frames, nil, nil)
	cancel(httpstream.ErrServerShutdown)

	require.Equal(t, httpstream.EndServerShutdown, waitForReason(t, done))
}

func TestStreamTurnFramesReportsAClosedSubscriptionAsAnError(t *testing.T) {
	t.Parallel()

	frames := make(chan TurnFrame)
	rr := httptest.NewRecorder()

	done := runStreamTurnFrames(t.Context(), rr, frames, nil, nil)
	close(frames)

	require.Equal(t, httpstream.EndError, waitForReason(t, done))
}

// failingWriter fails every write, as a connection the client dropped does.
type failingWriter struct {
	http.ResponseWriter
}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestStreamTurnFramesEndsOnAFailedWrite(t *testing.T) {
	t.Parallel()

	frames := make(chan TurnFrame)
	heartbeat := make(chan time.Time, 1)

	done := runStreamTurnFrames(t.Context(), failingWriter{ResponseWriter: httptest.NewRecorder()}, frames, heartbeat, nil)
	heartbeat <- time.Now()

	require.Equal(t, httpstream.EndClientClosed, waitForReason(t, done))
}
