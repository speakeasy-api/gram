package remotesessions_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type failingAddCache struct {
	cache.Cache
}

func (failingAddCache) Add(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("cache unavailable")
}

// A definitive invalid_grant invalidates the refresh grant, not necessarily
// the current access token. RefreshNow clears the dead grant without deleting
// the session, so proactive refresh cannot force a reconnect.
func TestRefreshNow_InvalidGrant_ClearsRefreshGrantWhenCacheUnavailable(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "refreshnow-invalid-grant", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"dead-refresh"}`))
			return
		}
		refreshAttempts.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	})
	env.refresher = env.newRefresher(failingAddCache{Cache: cache.NoopCache})

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	result, err := env.refresher.RefreshNow(ctx, session, "")
	require.Error(t, err)
	require.Empty(t, result.Outcome)
	require.Empty(t, result.AccessToken)

	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.False(t, active.RefreshTokenEncrypted.Valid)
	require.False(t, active.RefreshExpiresAt.Valid)

	statuses, err := env.mgr.RemoteSessionStatuses(
		ctx,
		env.subject,
		[]uuid.UUID{env.clientID},
	)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[env.clientID].Status)
	require.False(t, statuses[env.clientID].CanRefresh)

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "expired-access", resolved, "unknown-expiry access remains usable")
	require.EqualValues(t, 1, refreshAttempts.Load(), "cache failure must not prevent the upstream attempt")
}

func TestRefreshNow_InvalidGrant_DatadogErrorsArray_ClearsRefreshGrant(t *testing.T) {
	t.Parallel()

	assertRefreshNowClearsDeadGrant(
		t,
		"refreshnow-datadog-errors",
		http.StatusBadRequest,
		`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`,
		"invalid_grant: Invalid or expired refresh token or code verifier.",
	)
}

func TestRefreshNow_InvalidGrant_DubUnauthorized_ClearsRefreshGrant(t *testing.T) {
	t.Parallel()

	assertRefreshNowClearsDeadGrant(
		t,
		"refreshnow-dub-unauthorized",
		http.StatusUnauthorized,
		`{"error":{"code":"unauthorized","message":"Refresh token not found."}}`,
		"invalid_grant: Refresh token not found.",
	)
}

func TestRefreshNow_InvalidGrant_Generic4xxTokenMention_ClearsRefreshGrant(t *testing.T) {
	t.Parallel()

	assertRefreshNowClearsDeadGrant(
		t,
		"refreshnow-generic-4xx",
		http.StatusBadRequest,
		`token endpoint rejected refresh: invalid_grant`,
		"invalid_grant",
	)
}

func assertRefreshNowClearsDeadGrant(t *testing.T, slugSuffix string, status int, body string, wantReason string) {
	t.Helper()

	ctx, env := newSyntheticExpiryEnv(t, slugSuffix, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"dead-refresh"}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	result, err := env.refresher.RefreshNow(ctx, session, "")
	require.Error(t, err)
	require.Empty(t, result.Outcome)
	require.Empty(t, result.AccessToken)

	var tokenErr *remotesessions.TokenRefreshError
	require.ErrorAs(t, err, &tokenErr)
	require.Equal(t, wantReason, tokenErr.Reason)

	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.False(t, active.RefreshTokenEncrypted.Valid)
	require.False(t, active.RefreshExpiresAt.Valid)
}
