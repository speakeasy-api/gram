package remotesessions_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// unreadableTokenProvider answers a code exchange and counts refreshes, which return a new pair.
func unreadableTokenProvider(refreshes *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshes.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"healed-access","refresh_token":"healed-refresh","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"live-access","refresh_token":"live-refresh"}`))
	}
}

func storeUnreadableTokens(t *testing.T, env syntheticExpiryEnv, refreshEncrypted string) {
	t.Helper()
	_, err := env.q.UpdateRemoteSessionTokensIfUnchanged(t.Context(), repo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn: env.subject, RemoteSessionClientID: env.clientID,
		ExpectedUpdatedAt: env.session.UpdatedAt, AccessTokenEncrypted: "not-ciphertext",
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), RefreshTokenEncrypted: conv.ToPGText(refreshEncrypted),
		Scopes: env.session.Scopes,
	})
	require.NoError(t, err)
}

func storedTokenUnreadableCount(t *testing.T, env syntheticExpiryEnv, token remotesessionmetrics.StoredToken, outcome remotesessionmetrics.StoredTokenOutcome) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, env.registrationTelemetryReader.Collect(t.Context(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gram.remote_session.stored_token_unreadable" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				gotToken, _ := point.Attributes.Value(attr.OAuthStoredTokenKey)
				gotOutcome, _ := point.Attributes.Value(attr.OutcomeKey)
				if gotToken.AsString() == string(token) && gotOutcome.AsString() == string(outcome) {
					return point.Value
				}
			}
		}
	}
	return 0
}

// A refresh replaces an unreadable access token under the current key, unseen by the person.
func TestResolveAccessTokens_UnreadableAccessTokenHealsByRefresh(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	var logs bytes.Buffer
	ctx, env := newSyntheticExpiryEnv(t, "unreadable-access-heals", unreadableTokenProvider(&refreshes),
		withRegistrationTelemetry(), withLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	storeUnreadableTokens(t, env, env.session.RefreshTokenEncrypted.String)

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.False(t, reconnect, "a readable refresh token renews it")

	tokens, err := env.mgr.ResolveAccessTokens(ctx, remotesessions.RefreshAttribution{}, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	for _, entry := range tokens {
		require.Equal(t, "healed-access", entry.Token)
	}
	require.EqualValues(t, 1, refreshes.Load())

	stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	plain, err := env.enc.Decrypt(stored.AccessTokenEncrypted)
	require.NoError(t, err, "the new token is stored under the current key")
	require.Equal(t, "healed-access", plain)

	require.EqualValues(t, 1, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenHealed))
	require.Contains(t, logs.String(), "stored access token could not be decrypted")
}

// With nothing readable, only a new sign-in recreates the grant: a reconnect, never a call without it.
func TestResolveAccessTokens_UnreadableSignInNeedsReconnect(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	var logs bytes.Buffer
	ctx, env := newSyntheticExpiryEnv(t, "unreadable-sign-in", unreadableTokenProvider(&refreshes),
		withRegistrationTelemetry(), withLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	storeUnreadableTokens(t, env, "not-ciphertext")

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.True(t, reconnect)

	_, err = env.mgr.ResolveAccessTokens(ctx, remotesessions.RefreshAttribution{}, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrStoredSignInUnreadable)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)

	tokens, err := env.mgr.ResolveAvailableAccessTokens(ctx, remotesessions.RefreshAttribution{}, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.Len(t, tokens, 1, "a gateway keeps the member's entry so it never calls it without credentials")
	for _, entry := range tokens {
		require.Empty(t, entry.Token)
		require.True(t, entry.SignInUnreadable())
		require.False(t, entry.SessionUnavailable(), "not an outage to retry")
	}
	require.Zero(t, refreshes.Load(), "an unreadable refresh token is never sent")

	require.Positive(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Positive(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Contains(t, logs.String(), "stored refresh token could not be decrypted")
}
