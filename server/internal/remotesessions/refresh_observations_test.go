package remotesessions_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Two requests that need one refresh at once make one provider POST, and each
// request's sink hears its own logical outcome: the lock holder refreshed and
// the other adopted its result. The provider answers only once the second
// refresh has started reporting, so it is known to hold the stale snapshot.
func TestRefreshObservations_ConcurrentWinnerAndAdopter(t *testing.T) {
	t.Parallel()

	var posts atomic.Int64
	reached, held := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	var once sync.Once
	ctx, env := newSyntheticExpiryEnv(t, "refresh-observe-race", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"live-refresh","expires_in":1}`))
			return
		}
		posts.Add(1)
		once.Do(func() { close(reached) })
		<-held
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"rotated-refresh","expires_in":3600}`))
	})
	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
	}))
	stale, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)

	// Registered after the token server, so it runs before the server closes:
	// a failed assertion never leaves the provider handler held.
	t.Cleanup(release)

	winner, adopter := remotesessions.NewRefreshObservations(4), remotesessions.NewRefreshObservations(4)
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = env.refresher.RefreshNow(remotesessions.WithRefreshObservations(ctx, winner), stale, "", remotesessionmetrics.RefreshTriggerRequest)
	})
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the first refresh never reached the provider")
	}
	wg.Go(func() {
		_, _ = env.refresher.RefreshNow(remotesessions.WithRefreshObservations(ctx, adopter), stale, "", remotesessionmetrics.RefreshTriggerRequest)
	})
	require.Eventually(t, func() bool { return adopter.Reserved() == 1 }, 10*time.Second, 10*time.Millisecond)
	release()
	wg.Wait()

	won, dropped := winner.Close()
	require.Zero(t, dropped)
	adopted, dropped := adopter.Close()
	require.Zero(t, dropped)
	require.EqualValues(t, 1, posts.Load(), "only the lock holder contacted the provider")
	require.Len(t, won, 1)
	require.Len(t, adopted, 1)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, won[0].Outcome)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeAdoptedConcurrentWinner, adopted[0].Outcome)
	require.True(t, won[0].Finished && adopted[0].Finished)
	require.True(t, won[0].ProviderIssuerID.Valid)
	require.Equal(t, won[0].ProviderIssuerID, adopted[0].ProviderIssuerID)
}

// A refresh the scheduler or a person starts is never attributed to a
// request, even under a context that carries a sink.
func TestRefreshObservations_OnlyRequestRefreshesReport(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refresh-observe-trigger", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"live-refresh","expires_in":1}`))
	})
	sink := remotesessions.NewRefreshObservations(4)
	observed := remotesessions.WithRefreshObservations(ctx, sink)
	for _, trigger := range []remotesessionmetrics.RefreshTrigger{remotesessionmetrics.RefreshTriggerScheduled, remotesessionmetrics.RefreshTriggerManual} {
		_, _ = env.refresher.RefreshNow(observed, env.session, "", trigger)
	}
	observations, dropped := sink.Close()
	require.Empty(t, observations)
	require.Zero(t, dropped)
}
