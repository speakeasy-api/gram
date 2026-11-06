package remotesessions_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// unreadableTokenProvider answers a code exchange, and a refresh with refresh.
func unreadableTokenProvider(refreshes *atomic.Int32, refresh http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshes.Add(1)
			refresh(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"live-access","refresh_token":"live-refresh"}`))
	}
}

func healingRefresh(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"access_token":"healed-access","refresh_token":"healed-refresh","expires_in":3600}`))
}

// unreadableEnv is a synthetic login whose log lines and metrics a test reads.
func unreadableEnv(t *testing.T, slug string, refreshes *atomic.Int32, refresh http.HandlerFunc) (syntheticExpiryEnv, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	_, env := newSyntheticExpiryEnv(t, slug, unreadableTokenProvider(refreshes, refresh),
		withRegistrationTelemetry(), withLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	return env, logs
}

// lockedBuffer is a log sink concurrent requests can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write log line: %w", err)
	}
	return n, nil
}

// levelOf is the level of the first log line carrying message, empty when none does.
func (b *lockedBuffer) levelOf(message string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for line := range strings.SplitSeq(b.buf.String(), "\n") {
		var entry struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && strings.HasPrefix(entry.Msg, message) {
			return entry.Level
		}
	}
	return ""
}

// storeTokens overwrites the stored pair; a plaintext of "" stores ciphertext the key can't read.
func storeTokens(t *testing.T, env syntheticExpiryEnv, access, refresh string, accessExpiresIn time.Duration) {
	t.Helper()
	encrypt := func(plain string) string {
		if plain == "" {
			return "not-ciphertext"
		}
		ciphertext, err := env.enc.Encrypt([]byte(plain))
		require.NoError(t, err)
		return ciphertext
	}
	current, err := env.q.GetActiveRemoteSession(t.Context(), repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	_, err = env.q.UpdateRemoteSessionTokensIfUnchanged(t.Context(), repo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn: env.subject, RemoteSessionClientID: env.clientID,
		ExpectedUpdatedAt: current.UpdatedAt, AccessTokenEncrypted: encrypt(access),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(accessExpiresIn)), RefreshTokenEncrypted: conv.ToPGText(encrypt(refresh)),
		Scopes: current.Scopes,
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

func resolveAll(t *testing.T, env syntheticExpiryEnv) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	t.Helper()
	tokens, err := env.mgr.ResolveAccessTokens(t.Context(), remotesessions.RefreshAttribution{}, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	if err != nil {
		return nil, fmt.Errorf("resolve access tokens: %w", err)
	}
	return tokens, nil
}

func resolveAvailable(t *testing.T, env syntheticExpiryEnv) remotesessions.UpstreamToken {
	t.Helper()
	tokens, err := env.mgr.ResolveAvailableAccessTokens(t.Context(), remotesessions.RefreshAttribution{}, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	for _, entry := range tokens {
		return entry
	}
	return remotesessions.UpstreamToken{}
}

func storedSessionStatus(t *testing.T, env syntheticExpiryEnv) remotesessions.RemoteSessionState {
	t.Helper()
	statuses, err := env.mgr.RemoteSessionStatuses(t.Context(), env.subject, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	state, ok := statuses[env.clientID]
	require.True(t, ok)
	return state
}

func needsReconnect(t *testing.T, env syntheticExpiryEnv) bool {
	t.Helper()
	reconnect, err := env.mgr.RemoteSessionsNeedReconnect(t.Context(), env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.subject)
	require.NoError(t, err)
	return reconnect
}

// A refresh replaces an unreadable access token under the current key, unseen by the person.
func TestResolveAccessTokens_UnreadableAccessTokenHealsByRefresh(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, logs := unreadableEnv(t, "unreadable-access-heals", &refreshes, healingRefresh)
	storeTokens(t, env, "", "live-refresh", time.Hour)

	require.False(t, needsReconnect(t, env), "a readable refresh token renews it")
	require.Equal(t, remotesessions.RemoteSessionActive, storedSessionStatus(t, env).Status)

	tokens, err := resolveAll(t, env)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	for _, entry := range tokens {
		require.Equal(t, "healed-access", entry.Token)
	}
	require.EqualValues(t, 1, refreshes.Load())

	stored, err := env.q.GetActiveRemoteSession(t.Context(), repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	plain, err := env.enc.Decrypt(stored.AccessTokenEncrypted)
	require.NoError(t, err, "the new token is stored under the current key")
	require.Equal(t, "healed-access", plain)

	require.EqualValues(t, 1, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenHealed))
	require.Zero(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Equal(t, "ERROR", logs.levelOf("remote session stored access token could not be decrypted"))
}

// With nothing readable, only a new sign-in recreates the grant: a reconnect, never a call without it.
func TestResolveAccessTokens_UnreadableSignInNeedsReconnect(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, logs := unreadableEnv(t, "unreadable-sign-in", &refreshes, healingRefresh)
	storeTokens(t, env, "", "", time.Hour)

	require.True(t, needsReconnect(t, env))
	state := storedSessionStatus(t, env)
	require.Equal(t, remotesessions.RemoteSessionExpired, state.Status, "status must not say connected while resolution can't read it")
	require.False(t, state.CanRefresh)

	_, err := resolveAll(t, env)
	require.ErrorIs(t, err, remotesessions.ErrStoredSignInUnreadable)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)

	entry := resolveAvailable(t, env)
	require.Empty(t, entry.Token)
	require.True(t, entry.SignInUnreadable(), "a gateway keeps the member's entry so it never calls it without credentials")
	require.True(t, entry.HeldWithoutToken())
	require.False(t, entry.SessionUnavailable(), "not an outage to retry")
	require.Zero(t, refreshes.Load(), "an unreadable refresh token is never sent")

	require.EqualValues(t, 2, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.EqualValues(t, 2, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Equal(t, "ERROR", logs.levelOf("remote session stored sign-in could not be decrypted"))
}

// Concurrent requests on an unreadable row never take the refresh lock, so none waits it out or reads it as an outage.
func TestResolveAccessTokens_UnreadableSignInConcurrentCallers(t *testing.T) {
	t.Parallel()

	const callers = 8
	var refreshes atomic.Int32
	env, _ := unreadableEnv(t, "unreadable-concurrent", &refreshes, healingRefresh)
	storeTokens(t, env, "", "", time.Hour)

	started := time.Now()
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { _, errs[i] = resolveAll(t, env) })
	}
	wg.Wait()

	require.Less(t, time.Since(started), 5*time.Second, "no caller waits on a refresh lock")
	for _, err := range errs {
		require.ErrorIs(t, err, remotesessions.ErrStoredSignInUnreadable)
		require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	}
	require.Zero(t, refreshes.Load())
	require.EqualValues(t, callers, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenNeedsSignIn))
}

// An expired access token with an unreadable refresh token needs a new sign-in.
func TestResolveAccessTokens_ExpiredAccessUnreadableRefreshNeedsReconnect(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, logs := unreadableEnv(t, "unreadable-refresh-expired", &refreshes, healingRefresh)
	storeTokens(t, env, "stale-access", "", -time.Hour)

	require.True(t, needsReconnect(t, env))
	require.Equal(t, remotesessions.RemoteSessionExpired, storedSessionStatus(t, env).Status)
	_, err := resolveAll(t, env)
	require.ErrorIs(t, err, remotesessions.ErrStoredSignInUnreadable)
	require.True(t, resolveAvailable(t, env).SignInUnreadable())
	require.Zero(t, refreshes.Load())
	require.EqualValues(t, 2, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Zero(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Equal(t, "ERROR", logs.levelOf("remote session stored sign-in could not be decrypted"))
}

// Inside the expiry skew an unreadable refresh token is never sent: the live access token serves, and nobody is told to sign in yet.
func TestResolveAccessTokens_UnreadableRefreshInsideSkewForwardsAccess(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, logs := unreadableEnv(t, "unreadable-refresh-skew", &refreshes, healingRefresh)
	storeTokens(t, env, "skew-access", "", remotesessions.AccessTokenExpirySkew/2)

	require.False(t, needsReconnect(t, env))
	require.Equal(t, remotesessions.RemoteSessionActive, storedSessionStatus(t, env).Status)
	tokens, err := resolveAll(t, env)
	require.NoError(t, err)
	for _, entry := range tokens {
		require.Equal(t, "skew-access", entry.Token)
	}
	require.Zero(t, refreshes.Load())
	require.EqualValues(t, 1, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenAccessStillLive))
	require.Zero(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Equal(t, "WARN", logs.levelOf("remote session stored refresh token could not be decrypted"))
}

// A refused renewal of an unreadable access token leaves nothing usable: a new sign-in, never an anonymous call.
func TestResolveAccessTokens_UnreadableAccessRefusedRenewalNeedsReconnect(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, _ := unreadableEnv(t, "unreadable-access-refused", &refreshes, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	storeTokens(t, env, "", "live-refresh", time.Hour)

	entry := resolveAvailable(t, env)
	require.Empty(t, entry.Token)
	require.True(t, entry.SignInUnreadable())
	require.EqualValues(t, 1, refreshes.Load())
	require.EqualValues(t, 1, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn))
}

// An outage renewing an unreadable access token stays an outage: nobody is told to sign in again.
func TestResolveAccessTokens_UnreadableAccessRenewalOutageStaysOutage(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, _ := unreadableEnv(t, "unreadable-access-outage", &refreshes, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	storeTokens(t, env, "", "live-refresh", time.Hour)

	entry := resolveAvailable(t, env)
	require.True(t, entry.SessionUnavailable())
	require.False(t, entry.SignInUnreadable())
	require.Zero(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn))
	require.Zero(t, storedTokenUnreadableCount(t, env, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenHealed))
}

// A waiter that finds the row moved to a token it still can't read renews it rather than reporting an outage.
func TestRefreshNow_MovedUnreadableRowIsRenewed(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, _ := unreadableEnv(t, "unreadable-moved-row", &refreshes, healingRefresh)
	stale := env.session
	storeTokens(t, env, "", "live-refresh", time.Hour)

	res, err := env.refresher.RefreshNow(t.Context(), stale, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshAttribution{})
	require.NoError(t, err)
	require.Equal(t, "healed-access", res.AccessToken)
	require.EqualValues(t, 1, refreshes.Load())
}

// Connecting again through the sign-in flow rewrites an unreadable row, and the next call resolves.
func TestReconnect_RewritesUnreadableSignIn(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	env, _ := unreadableEnv(t, "unreadable-reconnect", &refreshes, healingRefresh)
	storeTokens(t, env, "", "", time.Hour)
	require.Equal(t, remotesessions.RemoteSessionExpired, storedSessionStatus(t, env).Status)

	clients, err := env.mgr.ListClients(t.Context(), env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	subject := env.subject
	authURL, err := env.mgr.BuildAuthorizationUrl(t.Context(), remotesessions.ParentChallenge{
		ID: uuid.NewString(), ProjectID: env.projectID, OrganizationID: env.organizationID,
		UserSessionIssuerID: env.session.UserSessionIssuerID, Subject: &subject, McpSlug: "unreadable-reconnect",
		McpServerID: uuid.NullUUID{}, ResourceMcpServerID: uuid.NullUUID{}, FinalRedirectURI: "", Resource: "", ClientName: "",
	}, clients[0])
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	callback, err := env.callback(t, url.Values{"code": {"upstream-code"}, "state": {parsed.Query().Get("state")}}.Encode())
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, callback.Code)

	require.Equal(t, remotesessions.RemoteSessionActive, storedSessionStatus(t, env).Status)
	tokens, err := resolveAll(t, env)
	require.NoError(t, err)
	for _, entry := range tokens {
		require.Equal(t, "live-access", entry.Token)
	}
	require.False(t, needsReconnect(t, env))
}
