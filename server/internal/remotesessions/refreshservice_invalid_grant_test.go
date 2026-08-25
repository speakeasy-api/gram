package remotesessions_test

import (
	"context"
	"errors"
	"net/http"
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
	env.refresher = env.newRefresher(testenv.NewMeterProvider(t), failingAddCache{Cache: cache.NoopCache})

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	result, err := env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerScheduled)
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
		env.projectID,
		env.organizationID,
		env.session.UserSessionIssuerID,
	)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[env.clientID].Status)
	require.False(t, statuses[env.clientID].CanRefresh)

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "expired-access", resolved, "unknown-expiry access remains usable")
	require.EqualValues(t, 1, refreshAttempts.Load(), "cache failure must not prevent the upstream attempt")
}

// Datadog's token endpoint wraps the RFC 6749 §5.2 invalid_grant in its
// API-wide {"errors": [string]} envelope; the flattened code is still
// definitive and clears the dead grant.
func TestRefreshNow_InvalidGrant_DatadogErrorsArray_ClearsRefreshGrant(t *testing.T) {
	t.Parallel()

	active, tokenErr := refreshNowAgainstUpstreamError(
		t,
		"refreshnow-datadog-errors",
		http.StatusBadRequest,
		`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`,
	)

	require.Equal(t, "invalid_grant: Invalid or expired refresh token or code verifier.", tokenErr.Reason)
	require.False(t, active.RefreshTokenEncrypted.Valid)
	require.False(t, active.RefreshExpiresAt.Valid)
}

// Dub never emits invalid_grant; its exact "Refresh token not found" message
// is the last-chance vendor signal that clears the dead grant.
func TestRefreshNow_InvalidGrant_DubRefreshTokenNotFound_ClearsRefreshGrant(t *testing.T) {
	t.Parallel()

	active, tokenErr := refreshNowAgainstUpstreamError(
		t,
		"refreshnow-dub-dead-grant",
		http.StatusUnauthorized,
		`{"error":{"code":"unauthorized","message":"Refresh token not found.","doc_url":"https://dub.co/docs/api-reference/errors#unauthorized"}}`,
	)

	require.Equal(t, "invalid_grant: Refresh token not found.", tokenErr.Reason)
	require.False(t, active.RefreshTokenEncrypted.Valid)
	require.False(t, active.RefreshExpiresAt.Valid)
}

// A client authentication failure under the same vendor code is fixed by
// correcting the client configuration, so the still-valid refresh grant must
// survive it.
func TestRefreshNow_DubClientAuthFailure_KeepsRefreshGrant(t *testing.T) {
	t.Parallel()

	active, tokenErr := refreshNowAgainstUpstreamError(
		t,
		"refreshnow-dub-client-auth",
		http.StatusUnauthorized,
		`{"error":{"code":"unauthorized","message":"Invalid client_secret"}}`,
	)

	require.Equal(t, "unauthorized: Invalid client_secret", tokenErr.Reason)
	require.True(t, active.RefreshTokenEncrypted.Valid, "a recoverable failure must not clear the grant")
}

// refreshNowAgainstUpstreamError links a session whose access token has
// expired, makes the upstream token endpoint answer every refresh with the
// given status and body, runs RefreshNow once, and returns the session row as
// persisted afterwards together with the operator-actionable refresh error.
func refreshNowAgainstUpstreamError(t *testing.T, slugSuffix string, status int, body string) (repo.RemoteSession, *remotesessions.TokenRefreshError) {
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

	result, refreshErr := env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.Error(t, refreshErr)
	require.Empty(t, result.Outcome)
	require.Empty(t, result.AccessToken)
	var tokenErr *remotesessions.TokenRefreshError
	require.ErrorAs(t, refreshErr, &tokenErr)

	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	return active, tokenErr
}
