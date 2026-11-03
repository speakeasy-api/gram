package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
		name   string
		status int
		body   string
		want   mcpconnectionattempts.Happened
		who    mcpconnectionattempts.WhoFixes
	}{
		{name: "refreshed", status: http.StatusOK, body: `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`, want: mcpconnectionattempts.ResultRefreshed, who: mcpconnectionattempts.WhoNobody},
		{name: "invalid grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`, want: mcpconnectionattempts.CodeInvalidGrant, who: mcpconnectionattempts.WhoPerson},
		{name: "invalid client without a registration endpoint", status: http.StatusUnauthorized, body: `{"error":"invalid_client"}`, want: mcpconnectionattempts.CodeProviderOAuthInvalidClient, who: mcpconnectionattempts.WhoAdmin},
		{name: "rejected", status: http.StatusBadRequest, body: `{"error":"unauthorized_client"}`, want: mcpconnectionattempts.CodeRefreshRejected, who: mcpconnectionattempts.WhoAdmin},
		{name: "rejected with an unreadable body", status: http.StatusBadRequest, body: `nope`, want: mcpconnectionattempts.CodeRefreshRejected, who: mcpconnectionattempts.WhoAdmin},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, want: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider},
		{name: "provider outage", status: http.StatusServiceUnavailable, body: `{}`, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, env := newSyntheticExpiryEnv(t, "refresh-attempts", tokenAnswer(tc.status, tc.body))
			rec := mcpconnectionattemptstest.New(t)
			refresher := env.newRefresher(testenv.NewMeterProvider(t), cache.NoopCache)
			remotesessions.WithRefreshConnectionRecorder(rec.Recorder)(refresher)

			session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)

			serverID := uuid.New()
			scope := mcpconnectionattempts.ServerScope(env.organizationID, env.projectID, serverID, mcpconnectionattempts.AppDCR)
			_, _ = refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshIn(scope))

			row := rec.RequireEnded(mcpconnectionattempts.StepProviderRefresh, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Equal(t, env.organizationID, row.OrganizationID)
			require.Equal(t, serverID.String(), row.ServerID)
			require.Equal(t, "idp.example.com", row.ProviderHost)
			require.Equal(t, session.ID.String(), row.RemoteSessionID)
			require.Equal(t, env.subject.String(), row.SubjectURN)
			require.Equal(t, string(remotesessionmetrics.RefreshTriggerRequest), row.SubReason)
			require.Len(t, rec.Rows(), 1)
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

	_, _ = refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerManual, remotesessions.RefreshAttribution{})
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
	_, _ = refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest, remotesessions.RefreshIn(mcpconnectionattempts.ProviderSessionScope(env.organizationID, env.projectID, mcpconnectionattempts.AppDCR)).InAttempt(attempt))

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
