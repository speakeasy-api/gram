package remotesessions_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newExpiredRefreshEnv links a session whose access token has already expired,
// so the next resolution must refresh against refreshHandler.
func newExpiredRefreshEnv(t *testing.T, slugSuffix string, refreshHandler http.HandlerFunc) (context.Context, syntheticExpiryEnv) {
	t.Helper()

	ctx, env := newSyntheticExpiryEnv(t, slugSuffix, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshHandler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"live-refresh"}`))
	})

	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
	}))
	return ctx, env
}

func TestResolveAccessTokens_UpstreamServerErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newExpiredRefreshEnv(t, "refresh-upstream-5xx", func(w http.ResponseWriter, _ *http.Request) {
		refreshAttempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "callers that only ask for a usable token still read it as absent")

	// The grant survives a transient failure, so the next request retries it.
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.True(t, active.RefreshTokenEncrypted.Valid)

	_, err = env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	require.EqualValues(t, 2, refreshAttempts.Load())
}

func TestResolveAccessToken_UpstreamServerErrorIsNoToken(t *testing.T) {
	t.Parallel()

	ctx, env := newExpiredRefreshEnv(t, "refresh-upstream-5xx-single", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err, "the single-client primitive reports every unusable token as empty")
	require.Empty(t, token)
}

func TestResolveAvailableAccessTokens_UpstreamServerErrorSkipsClient(t *testing.T) {
	t.Parallel()

	ctx, env := newExpiredRefreshEnv(t, "refresh-upstream-5xx-available", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	tokens, err := env.mgr.ResolveAvailableAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.Empty(t, tokens)
}

func TestResolveAccessTokens_UnauthorizedClientStaysMisconfigured(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newExpiredRefreshEnv(t, "refresh-unauthorized-client", func(w http.ResponseWriter, _ *http.Request) {
		refreshAttempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
	})

	// The upstream refused the client's request, not the grant, so the grant
	// stays stored and every request reports the same administrator remedy.
	for range 2 {
		_, err := env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
		require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	}
	require.EqualValues(t, 2, refreshAttempts.Load())

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.False(t, reconnect, "re-linking cannot repair a refused client request")
}

func TestResolveAccessTokens_InvalidClientNeedsReconnect(t *testing.T) {
	t.Parallel()

	ctx, env := newExpiredRefreshEnv(t, "refresh-invalid-client", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	})

	// invalid_client marks the client for re-registration at the next login
	// and clears the grant, so re-linking is the repair.
	_, err := env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.True(t, reconnect)
}

func TestResolveAccessTokens_InvalidGrantNeedsReconnect(t *testing.T) {
	t.Parallel()

	ctx, env := newExpiredRefreshEnv(t, "refresh-invalid-grant-refined", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	_, err := env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)

	// The failed refresh cleared the grant, so stored state now agrees.
	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.True(t, reconnect)
}

// An unreadable stored access token is an error validateAndRefresh raises
// itself rather than a RefreshError, so it never clears on retry and must keep
// the reconnect remedy.
func TestResolveAccessTokens_UnreadableAccessTokenNeedsReconnect(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "unreadable-access-token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshAttempts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"live-access","refresh_token":"live-refresh"}`))
	})

	_, err := env.q.UpdateRemoteSessionTokensIfUnchanged(ctx, repo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn: env.subject, RemoteSessionClientID: env.clientID,
		ExpectedUpdatedAt: env.session.UpdatedAt, AccessTokenEncrypted: "not-ciphertext",
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), RefreshTokenEncrypted: env.session.RefreshTokenEncrypted,
		Scopes: env.session.Scopes,
	})
	require.NoError(t, err)

	_, err = env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	require.Zero(t, refreshAttempts.Load(), "a live access token is forwarded without refreshing")

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.True(t, reconnect)
}

func TestRemoteSessionsNeedReconnect_RenewableGrantDoesNotContactUpstream(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newExpiredRefreshEnv(t, "reconnect-check-renewable", func(w http.ResponseWriter, _ *http.Request) {
		refreshAttempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.False(t, reconnect, "an expired access token with a refresh grant is still renewable")
	require.Zero(t, refreshAttempts.Load(), "the reconnect check must never refresh")
}

func TestRemoteSessionsNeedReconnect_MissingSession(t *testing.T) {
	t.Parallel()

	ctx, env := newExpiredRefreshEnv(t, "reconnect-check-missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, urn.NewUserSubject("never-linked-"+uuid.NewString()))
	require.NoError(t, err)
	require.True(t, reconnect)
}

// A client that needs re-linking outranks another client's failed refresh,
// and resolution judges it from stored state instead of refreshing it too.
func TestResolveAccessTokens_ReconnectOutranksUnavailableWithOneRefresh(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newExpiredRefreshEnv(t, "rank-reconnect-over-unavailable", func(w http.ResponseWriter, _ *http.Request) {
		refreshAttempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	// A second upstream bound to the same issuer that the subject never linked.
	seedActiveClient(t, ctx, env.db, env.projectID, env.session.UserSessionIssuerID, env.organizationID, "rank-unlinked-"+uuid.NewString()[:8])

	_, err := env.mgr.ResolveAccessTokens(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable, "the user can repair the unlinked upstream, so it must win")
	require.LessOrEqual(t, refreshAttempts.Load(), int32(1))
}
