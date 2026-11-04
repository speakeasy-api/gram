package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func tokenAnswer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"first-access","refresh_token":"first-refresh"}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestRefreshNow_RecordsEachEndState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		status        int
		body          string
		want          mcpconnectionattempts.Happened
		who           mcpconnectionattempts.WhoFixes
		misconfigured bool
		truncated     bool
	}{
		{name: "refreshed", status: http.StatusOK, body: `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`, want: mcpconnectionattempts.ResultRefreshed, who: mcpconnectionattempts.WhoNobody},
		{name: "invalid grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`, want: mcpconnectionattempts.CodeInvalidGrant, who: mcpconnectionattempts.WhoPerson},
		{name: "invalid client without a registration endpoint", status: http.StatusUnauthorized, body: `{"error":"invalid_client"}`, want: mcpconnectionattempts.CodeProviderOAuthInvalidClient, who: mcpconnectionattempts.WhoAdmin},
		{name: "rejected", status: http.StatusBadRequest, body: `{"error":"unauthorized_client"}`, want: mcpconnectionattempts.CodeRefreshRejected, who: mcpconnectionattempts.WhoAdmin},
		{name: "rejected with an unreadable body", status: http.StatusBadRequest, body: `nope`, want: mcpconnectionattempts.CodeRefreshRejected, who: mcpconnectionattempts.WhoAdmin},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, want: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider},
		{name: "truncated success", truncated: true, status: http.StatusOK, body: `{`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "malformed success", status: http.StatusOK, body: `nope`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "missing access token", misconfigured: true, status: http.StatusOK, body: `{}`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "wrong access token type", status: http.StatusOK, body: `{"access_token":123}`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "server_error http.StatusOK", misconfigured: true, status: http.StatusOK, body: `{"error":"server_error"}`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "server_error http.StatusBadRequest", misconfigured: true, status: http.StatusBadRequest, body: `{"error":"server_error"}`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "temporarily_unavailable http.StatusOK", misconfigured: true, status: http.StatusOK, body: `{"error":"temporarily_unavailable"}`, want: mcpconnectionattempts.CodeProviderUnavailable, who: mcpconnectionattempts.WhoProvider},
		{name: "temporarily_unavailable http.StatusBadRequest", misconfigured: true, status: http.StatusBadRequest, body: `{"error":"temporarily_unavailable"}`, want: mcpconnectionattempts.CodeProviderUnavailable, who: mcpconnectionattempts.WhoProvider},
		{name: "login_required http.StatusOK", misconfigured: true, status: http.StatusOK, body: `{"error":"login_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "login_required http.StatusBadRequest", misconfigured: true, status: http.StatusBadRequest, body: `{"error":"login_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "interaction_required http.StatusOK", misconfigured: true, status: http.StatusOK, body: `{"error":"interaction_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "interaction_required http.StatusBadRequest", misconfigured: true, status: http.StatusBadRequest, body: `{"error":"interaction_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "consent_required http.StatusOK", misconfigured: true, status: http.StatusOK, body: `{"error":"consent_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "consent_required http.StatusBadRequest", misconfigured: true, status: http.StatusBadRequest, body: `{"error":"consent_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "account_selection_required http.StatusOK", misconfigured: true, status: http.StatusOK, body: `{"error":"account_selection_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "account_selection_required http.StatusBadRequest", misconfigured: true, status: http.StatusBadRequest, body: `{"error":"account_selection_required"}`, want: mcpconnectionattempts.CodeUpstreamReconnectRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "provider timeout", misconfigured: true, status: http.StatusRequestTimeout, body: `{}`, want: mcpconnectionattempts.CodeProviderUnreachable, who: mcpconnectionattempts.WhoProvider},
		{name: "provider outage", status: http.StatusServiceUnavailable, body: `{}`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer := tokenAnswer(tc.status, tc.body)
			ctx, env := newSyntheticExpiryEnv(t, "refresh-attempts", func(w http.ResponseWriter, r *http.Request) {
				if tc.truncated && r.FormValue("grant_type") == "refresh_token" {
					w.Header().Set("Content-Length", "100")
				}
				answer(w, r)
			})
			rec := mcpconnectionattemptstest.New(t)
			refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
			remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)

			session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)

			serverID := uuid.New()
			scope := mcpconnectionattempts.ServerScope(env.organizationID, env.projectID, serverID, mcpconnectionattempts.AppDCR)
			_, refreshErr := refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshIn(scope))
			require.Equal(t, refreshErr != nil, remotesessions.RefreshFailureRecorded(refreshErr))
			if refreshErr != nil {
				require.True(t, remotesessions.RefreshFailureRecorded(fmt.Errorf("wrapped: %w", refreshErr)))
			}
			if tc.misconfigured {
				require.ErrorIs(t, refreshErr, remotesessions.ErrRemoteSessionMisconfigured)
				require.NotErrorIs(t, refreshErr, remotesessions.ErrRemoteSessionUnavailable)
			}
			if tc.truncated {
				require.ErrorIs(t, refreshErr, remotesessions.ErrRemoteSessionUnavailable)
			}

			row := rec.RequireEnded(mcpconnectionattempts.StepProviderRefresh, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Equal(t, env.organizationID, row.OrganizationID)
			require.Equal(t, serverID.String(), row.ServerID)
			require.Equal(t, "idp.example.com", row.ProviderHost)
			require.Equal(t, session.ID.String(), row.RemoteSessionID)
			require.Equal(t, env.subject.String(), row.SubjectURN)
			require.Equal(t, string(remotesessionmetrics.RefreshTriggerRequest), row.SubReason)
			require.Len(t, rec.Rows(), 1)
			if tc.want == mcpconnectionattempts.CodeMalformedResponse || tc.want == mcpconnectionattempts.CodeProviderUnavailable || tc.want == mcpconnectionattempts.CodeProviderServerError {
				active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
				require.NoError(t, err)
				require.True(t, active.RefreshTokenEncrypted.Valid, "provider failures must preserve the refresh grant")
			}

		})
	}
}

// Nothing guesses a home for a row: a caller that names no scope gets no row.
func TestRefreshNow_ZeroAttributionWritesNoRow(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refresh-zero", tokenAnswer(http.StatusServiceUnavailable, `{}`))
	rec := mcpconnectionattemptstest.New(t)
	refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
	remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)

	_, refreshErr := refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerManual, remotesessions.RefreshAttribution{})
	require.False(t, remotesessions.RefreshFailureRecorded(refreshErr))
	rec.RequireNothing()
}

// A refresh inside a sign-in is a step of that attempt, so it cannot end it.
func TestRefreshNow_JoinsACarriedAttempt(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refresh-carried", tokenAnswer(http.StatusServiceUnavailable, `{}`))
	rec := mcpconnectionattemptstest.New(t)
	refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
	remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)

	attempt := mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(env.organizationID, env.projectID, uuid.New(), mcpconnectionattempts.AppDCR))
	_, refreshErr := refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshIn(mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR)).InAttempt(attempt))
	require.False(t, remotesessions.RefreshFailureRecorded(refreshErr))

	row := rec.RequireStep(mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeProviderServerError)
	require.Equal(t, attempt.ID(), row.AttemptID)
	rec.RequireNotEnded()
}

// An inline refresh names the caller's client; a sweep has no request to read.
func TestRefreshNow_CarriesTheRequestsClient(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refresh-client", tokenAnswer(http.StatusServiceUnavailable, `{}`))
	rec := mcpconnectionattemptstest.New(t)
	refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
	remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)

	scope := mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/server", nil)
	req.Header.Set("User-Agent", "claude-code/2.1.0")
	_, _ = refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshIn(scope).FromRequest(req))
	require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, rec.Rows()[0].ClientFamily)

	_, _ = refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerScheduled, remotesessions.RefreshIn(scope))
	require.Empty(t, rec.Rows()[1].ClientFamily)
}

// A session revoked mid-refresh and a caller that went away are neither success nor failure.
func TestRefreshNow_NotAnEndState(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refresh-canceled", tokenAnswer(http.StatusServiceUnavailable, `{}`))
	rec := mcpconnectionattemptstest.New(t)
	refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
	remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)

	session.ID = uuid.New()
	_, err = refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshAttribution{})
	require.NoError(t, err)
	rec.RequireNothing()
}

// A panic before the POST must still finish the connection attempt.
type refreshPanicCache struct{ cache.Cache }

func (refreshPanicCache) Get(context.Context, string, any) error { panic("refresh cache failed") }
func (refreshPanicCache) Add(context.Context, string, time.Duration) (bool, error) {
	panic("refresh cache failed")
}

func TestRefreshNow_RecordsRecoveredPanic(t *testing.T) {
	t.Parallel()
	for _, trigger := range []remotesessionmetrics.RefreshTrigger{remotesessionmetrics.RefreshTriggerRequest, remotesessionmetrics.RefreshTriggerManual, remotesessionmetrics.RefreshTriggerScheduled} {
		t.Run(string(trigger), func(t *testing.T) {
			t.Parallel()
			ctx, env := newSyntheticExpiryEnv(t, "refresh-panic", tokenAnswer(http.StatusOK, `{}`))
			rec := mcpconnectionattemptstest.New(t)
			refresher := env.newRefresher(testenv.NewMeterProvider(t), refreshPanicCache{Cache: cache.NoopCache})
			remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)
			session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)
			scope := mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR)
			_, err = refresher.RefreshNow(ctx, session, "", trigger, remotesessions.RefreshIn(scope))
			require.ErrorContains(t, err, "refresh panicked")
			rec.RequireEnded(mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeInternalError)
			require.Len(t, rec.Rows(), 1)

		})
	}
}

func TestRefreshNow_DetachedInvalidClientKeepsDynamicRemedy(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	answer := tokenAnswer(http.StatusBadRequest, `{"error":"invalid_client"}`)
	ctx, env := newSyntheticExpiryEnv(t, "detached-invalid-client", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") == "refresh_token" {
			close(started)
			<-release
		}
		answer(w, r)
	})
	_, err := env.db.Exec(ctx, "UPDATE remote_session_issuers SET registration_endpoint = $1 WHERE id = $2", "https://idp.example.com/register", env.issuerID)
	require.NoError(t, err)
	rec := mcpconnectionattemptstest.New(t)
	refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
	remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)
	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := refresher.RefreshNow(requestCtx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshIn(mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR)))
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh POST did not start")
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(release)
	require.NoError(t, refresher.DrainRefreshes(ctx))
	rec.RequireEnded(mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeRefreshInvalidClient)
	require.Len(t, rec.Rows(), 1)
}

func TestRefreshFailureRecorded_CachedOutageDoesNotReuseProvenance(t *testing.T) {
	t.Parallel()
	ctx, env := newSyntheticExpiryEnv(t, "recorded-cooldown", tokenAnswer(http.StatusServiceUnavailable, `{}`))
	rec := mcpconnectionattemptstest.New(t)
	refresher := env.newRefresher(testenv.NewMeterProvider(t), testenv.NewMemoryCache())
	remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)
	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	attribution := remotesessions.RefreshIn(mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR))
	_, fresh := refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, attribution)
	require.True(t, remotesessions.RefreshFailureRecorded(fresh))
	_, cached := refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, attribution)
	var failure *remotesessions.RefreshError
	require.ErrorAs(t, cached, &failure)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeDeferred, failure.Outcome)
	require.ErrorIs(t, cached, remotesessions.ErrRemoteSessionUnavailable)
	require.False(t, remotesessions.RefreshFailureRecorded(cached))
	require.False(t, remotesessions.RefreshFailureRecorded(fmt.Errorf("wrapped: %w", cached)))
	require.Len(t, rec.Rows(), 1)
}
func TestRefreshFailureRecorded_NoRecorder(t *testing.T) {
	t.Parallel()
	ctx, env := newSyntheticExpiryEnv(t, "recorded-no-recorder", tokenAnswer(http.StatusServiceUnavailable, `{}`))
	refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
	remotesessions.WithRefreshConnectionRecorder(nil)(refresher)
	_, err := refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerManual, remotesessions.RefreshIn(mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR)))
	require.Error(t, err)
	require.False(t, remotesessions.RefreshFailureRecorded(err))
}
