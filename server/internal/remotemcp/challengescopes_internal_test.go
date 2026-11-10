package remotemcp

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestChallengeScopesRecoversDetachedWritePanic(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	var manager ProxyManager
	manager.challengeScopes = newChallengeScopesState()
	// A nil pool forces a panic in the detached database call.
	finished := make(chan struct{}, 1)
	manager.afterChallengeScopes = func() { finished <- struct{}{} }
	projectID := uuid.New()
	resourceURL := "https://resource.example.test/mcp"
	for range 2 {
		manager.observeChallengeScopes(t.Context(), logger, projectID, resourceURL, http.StatusUnauthorized, []string{`Bearer scope="read"`})
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("detached write did not finish")
		}
		_, cached := manager.challengeScopes.seen.Load(projectID.String() + " " + resourceURL)
		require.False(t, cached, "a recovered panic must allow the next observation to retry")
		require.Eventually(t, func() bool { return len(manager.challengeScopes.slots) == 0 }, time.Second, time.Millisecond)
	}
	require.Contains(t, logs.String(), "record protected resource challenge scopes panicked")
}
