package remotesessions_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		token, err := env.mgr.ResolveAccessToken(requestCtx, env.clientID, env.subject, "")
		require.NoError(t, err)
		require.Empty(t, token)
	}()

	<-arrived
	cancelRequest()
	select {
	case <-returned:
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

func TestRefreshNow_ManualTriggerWaitsForTheResult(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	arrived := make(chan struct{})
	release := make(chan struct{})
	close(release)
	ctx, env := newExpiredRefreshEnv(t, "refresh-manual-cancel", rotatingProvider(&refreshes, arrived, release))
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	adminCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-arrived
		cancel()
	}()
	result, err := env.refresher.RefreshNow(adminCtx, active, "", remotesessionmetrics.RefreshTriggerManual)
	require.NoError(t, err, "the caller must learn the outcome so it can audit the refresh")
	require.Equal(t, "access-rotated", result.AccessToken)
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
