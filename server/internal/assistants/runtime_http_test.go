package assistants

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeRequestContextUsesMaxTimeSeconds(t *testing.T) {
	t.Parallel()

	ctx, cancel := runtimeRequestContext(context.Background(), 3, 2*time.Minute)
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(3*time.Second), deadline, 250*time.Millisecond)
}

func TestRuntimeManagerRuntimeRequestDirectHonorsMaxTimeSeconds(t *testing.T) {
	t.Parallel()

	server := newSlowAssistantRuntimeServer(t, 1500*time.Millisecond)

	manager := &RuntimeManager{}
	state := &runtimeState{
		apiBaseURL: server.URL,
		httpClient: server.Client(),
	}

	start := time.Now()
	_, err := manager.runtimeRequestDirect(context.Background(), state, runtimeHTTPRequest{
		Method:         http.MethodGet,
		Path:           "/slow",
		MaxTimeSeconds: 1,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "context deadline exceeded")
	require.Less(t, time.Since(start), 1400*time.Millisecond)
}

func newSlowAssistantRuntimeServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	return server
}
