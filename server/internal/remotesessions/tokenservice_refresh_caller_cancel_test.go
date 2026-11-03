package remotesessions_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// rotatingProvider consumes the refresh token on first use, as a rotating
// provider does, answering once release is closed if it is not nil.
func rotatingProvider(refreshes *atomic.Int32, arrived chan<- struct{}, release <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if refreshes.Add(1) > 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		close(arrived)
		<-release
		_, _ = w.Write([]byte(`{"access_token":"access-rotated","refresh_token":"refresh-rotated","token_type":"Bearer","expires_in":3600}`))
	}
}

func requireStoredRotation(t *testing.T, ctx context.Context, env syntheticExpiryEnv, refreshes *atomic.Int32) {
	t.Helper()

	env.mgr.WaitIdentityRestatements()
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.True(t, active.RefreshTokenEncrypted.Valid)
	stored, err := env.enc.Decrypt(active.RefreshTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "refresh-rotated", stored)

	token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-rotated", token)
	require.EqualValues(t, 1, refreshes.Load(), "the next request must use the stored token, not refresh again")
}

func TestResolveAccessToken_CallerCancelDuringRefreshKeepsRotatedToken(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	arrived := make(chan struct{})
	release := make(chan struct{})
	ctx, env := newExpiredRefreshEnv(t, "refresh-caller-cancel", rotatingProvider(&refreshes, arrived, release))

	requestCtx, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()
	type resolved struct {
		token string
		err   error
	}
	returned := make(chan resolved, 1)
	go func() {
		token, err := env.mgr.ResolveAccessToken(requestCtx, env.clientID, env.subject, "")
		returned <- resolved{token: token, err: err}
	}()

	<-arrived
	cancelRequest()
	select {
	case got := <-returned:
		require.NoError(t, got.err)
		require.Empty(t, got.token)
	case <-time.After(5 * time.Second):
		t.Fatal("the caller must stop waiting when its context ends")
	}
	close(release)

	requireStoredRotation(t, ctx, env, &refreshes)
}

func TestRefreshNow_CallerDeadlineHoldsWhileRefreshFinishes(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	arrived := make(chan struct{})
	release := make(chan struct{})
	ctx, env := newExpiredRefreshEnv(t, "refresh-caller-deadline", rotatingProvider(&refreshes, arrived, release))
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	budgetCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-arrived
		cancel()
	}()
	_, err = env.refresher.RefreshNow(budgetCtx, active, "", remotesessionmetrics.RefreshTriggerRequest)
	require.ErrorIs(t, err, context.Canceled)
	var refreshErr *remotesessions.RefreshError
	require.ErrorAs(t, err, &refreshErr)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeCanceled, refreshErr.Outcome)
	close(release)

	env.refresher.WaitIdentityRestatements()
	requireStoredRotation(t, ctx, env, &refreshes)
}

func TestDrainRefreshes_WaitsForARefreshThatOutlivedItsCaller(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	arrived := make(chan struct{})
	release := make(chan struct{})
	ctx, env := newExpiredRefreshEnv(t, "refresh-drain", rotatingProvider(&refreshes, arrived, release))

	requestCtx, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()
	go func() {
		<-arrived
		cancelRequest()
	}()
	_, err := env.mgr.ResolveAccessToken(requestCtx, env.clientID, env.subject, "")
	require.NoError(t, err)

	expired, cancelExpired := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancelExpired()
	require.ErrorIs(t, env.mgr.DrainRefreshes(expired), context.DeadlineExceeded, "the drain must respect its budget")

	close(release)
	require.NoError(t, env.mgr.DrainRefreshes(ctx))
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	stored, err := env.enc.Decrypt(active.RefreshTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "refresh-rotated", stored, "a completed drain means the rotated token is stored")
}

// leaseSpyCache records the lock lease a refresh holds.
type leaseSpyCache struct {
	cache.Cache
	mu      sync.Mutex
	ttl     time.Duration
	deleted bool
}

func (c *leaseSpyCache) Add(_ context.Context, _ string, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
	return true, nil
}

func (c *leaseSpyCache) Set(_ context.Context, _ string, _ any, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
	return nil
}

func (c *leaseSpyCache) Delete(context.Context, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = true
	return nil
}

func (c *leaseSpyCache) state() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ttl, c.deleted
}

func TestRefreshNow_HoldsTheLeaseUntilTheOutcomeIsStored(t *testing.T) {
	t.Parallel()

	const lockTTL = 12 * time.Second
	var refreshes atomic.Int32
	arrived := make(chan struct{})
	release := make(chan struct{})
	close(release)
	spy := &leaseSpyCache{Cache: cache.NoopCache}
	var ttlDuringPost atomic.Int64
	var deletedDuringPost atomic.Bool
	provider := rotatingProvider(&refreshes, arrived, release)
	ctx, env := newExpiredRefreshEnv(t, "refresh-lease", func(w http.ResponseWriter, r *http.Request) {
		ttl, deleted := spy.state()
		ttlDuringPost.Store(int64(ttl))
		deletedDuringPost.Store(deleted)
		provider(w, r)
	})
	refresher := env.newRefresher(testenv.NewMeterProvider(t), spy)
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	_, err = refresher.RefreshNow(ctx, active, "", remotesessionmetrics.RefreshTriggerManual)
	require.NoError(t, err)

	require.Greater(t, time.Duration(ttlDuringPost.Load()), lockTTL, "the lease must outlast the store, not only the POST")
	require.False(t, deletedDuringPost.Load())
	_, deleted := spy.state()
	require.True(t, deleted, "a finished refresh releases its extended lease")
}

func TestRefreshNow_ManualTriggerWaitsForTheResult(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	arrived := make(chan struct{})
	release := make(chan struct{})
	ctx, env := newExpiredRefreshEnv(t, "refresh-manual-cancel", rotatingProvider(&refreshes, arrived, release))
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	adminCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type refreshed struct {
		result remotesessions.RefreshResult
		err    error
	}
	returned := make(chan refreshed, 1)
	go func() {
		result, err := env.refresher.RefreshNow(adminCtx, active, "", remotesessionmetrics.RefreshTriggerManual)
		returned <- refreshed{result: result, err: err}
	}()

	<-arrived
	cancel()
	select {
	case <-returned:
		t.Fatal("a manual refresh must wait for the provider after its caller leaves")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	got := <-returned
	require.NoError(t, got.err, "the caller must learn the outcome so it can audit the refresh")
	require.Equal(t, "access-rotated", got.result.AccessToken)
}

func TestResolveAccessToken_CallerCancelStillClearsARefusedGrant(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{})
	release := make(chan struct{})
	ctx, env := newExpiredRefreshEnv(t, "refresh-cancel-refused", func(w http.ResponseWriter, _ *http.Request) {
		close(arrived)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	requestCtx, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()
	go func() {
		<-arrived
		cancelRequest()
	}()
	token, err := env.mgr.ResolveAccessToken(requestCtx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)
	close(release)

	env.mgr.WaitIdentityRestatements()
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.False(t, active.RefreshTokenEncrypted.Valid, "the provider's refusal is stored even after the caller left")
}
