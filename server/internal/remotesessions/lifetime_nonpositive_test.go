package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// reconnectSyntheticLogin runs a second sign-in for the subject and client of env.
func reconnectSyntheticLogin(t *testing.T, ctx context.Context, env syntheticExpiryEnv) repo.RemoteSession {
	t.Helper()
	clients, err := env.mgr.ListClients(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	subject := env.subject
	authURL, err := env.mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  env.parentID,
		ProjectID:           env.projectID,
		OrganizationID:      env.organizationID,
		UserSessionIssuerID: env.session.UserSessionIssuerID,
		Subject:             &subject,
		McpSlug:             "synthetic-mcp-reconnect",
		McpServerID:         env.mcpServerID,
		FinalRedirectURI:    "",
		Resource:            "",
	}, clients[0])
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	query := url.Values{"code": {"upstream-code"}, "state": {parsed.Query().Get("state")}}
	_, err = env.callback(t, query.Encode())
	require.NoError(t, err)
	return activeSession(t, ctx, env)
}

func activeSession(t *testing.T, ctx context.Context, env syntheticExpiryEnv) repo.RemoteSession {
	t.Helper()
	row, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	return row
}

func sessionStatus(t *testing.T, ctx context.Context, env syntheticExpiryEnv) remotesessions.RemoteSessionStatus {
	t.Helper()
	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	return states[env.clientID].Status
}

func requireNoToken(t *testing.T, ctx context.Context, env syntheticExpiryEnv) {
	t.Helper()
	token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)
}

// storeGrantDeadlines overwrites the grant's deadlines, as a row stored earlier would hold them.
func storeGrantDeadlines(t *testing.T, ctx context.Context, env syntheticExpiryEnv, access, refresh, authorization pgtype.Timestamptz) {
	t.Helper()
	row := activeSession(t, ctx, env)
	_, err := env.q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:             row.SubjectUrn,
		UserSessionIssuerID:    row.UserSessionIssuerID,
		RemoteSessionClientID:  row.RemoteSessionClientID,
		AccessTokenEncrypted:   row.AccessTokenEncrypted,
		AccessExpiresAt:        access,
		RefreshTokenEncrypted:  row.RefreshTokenEncrypted,
		AuthorizationExpiresAt: authorization,
		RefreshExpiresAt:       refresh,
		Scopes:                 row.Scopes,
		Resource:               row.Resource,
		AutoRefresh:            row.AutoRefresh,
		UpstreamSubject:        row.UpstreamSubject,
		UpstreamEmail:          row.UpstreamEmail,
		UpstreamDisplayName:    row.UpstreamDisplayName,
		IdentitySource:         row.IdentitySource,
		Enrichment:             row.Enrichment,
	})
	require.NoError(t, err)
}

// A sign-in that reports a zero or negative lifetime stores no deadline for it,
// so the grant works on arrival.
func TestRemoteLoginCallback_NonPositiveLifetimeIsUnreported(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		fields string
	}{
		{name: "zero authorization", fields: `"authorization_expires_in":0`},
		{name: "negative authorization", fields: `"authorization_expires_in":-1`},
		{name: "zero refresh timeout", fields: `"refresh_token_timeout":0`},
		{name: "negative refresh timeout", fields: `"refresh_token_timeout":-3600`},
		{name: "both zero", fields: `"authorization_expires_in":0,"refresh_token_timeout":0`},
		{name: "overflowing authorization", fields: `"authorization_expires_in":9223372036854775807`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tokenCalls atomic.Int64
			ctx, env := newSyntheticExpiryEnv(t, "nonpositive-"+uniqueSuffix(tc.name), func(w http.ResponseWriter, _ *http.Request) {
				tokenCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"access_token":"live-access","refresh_token":"live-refresh","token_type":"Bearer",%s}`, tc.fields)
			})

			require.False(t, env.session.AuthorizationExpiresAt.Valid)
			require.False(t, env.session.RefreshExpiresAt.Valid)
			require.Equal(t, remotesessions.RemoteSessionActive, sessionStatus(t, ctx, env))

			exchanged := tokenCalls.Load()
			token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
			require.NoError(t, err)
			require.Equal(t, "live-access", token)
			require.Equal(t, exchanged, tokenCalls.Load(), "a usable token needs no refresh")
		})
	}
}

// A positive lifetime is stored exactly and still ends the grant when it passes.
func TestRemoteLoginCallback_PositiveLifetimeExpiresOnSchedule(t *testing.T) {
	t.Parallel()

	before := time.Now()
	var tokenCalls atomic.Int64
	ctx, env := newSyntheticExpiryEnv(t, "positive-lifetime", func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"live-access","refresh_token":"live-refresh","token_type":"Bearer","expires_in":3600,"authorization_expires_in":60,"refresh_token_timeout":120}`))
	})
	require.WithinDuration(t, before.Add(time.Minute), env.session.AuthorizationExpiresAt.Time, 30*time.Second)
	require.WithinDuration(t, before.Add(2*time.Minute), env.session.RefreshExpiresAt.Time, 30*time.Second)
	require.Equal(t, remotesessions.RemoteSessionActive, sessionStatus(t, ctx, env))

	past := conv.ToPGTimestamptz(time.Now().Add(-time.Second))
	storeGrantDeadlines(t, ctx, env, env.session.AccessExpiresAt, env.session.RefreshExpiresAt, past)
	require.Equal(t, remotesessions.RemoteSessionExpired, sessionStatus(t, ctx, env))
	requireNoToken(t, ctx, env)
}

// A stored deadline that has passed stays final, and no refresh is tried against it.
func TestGenuinelyExpiredGrantStaysExpired(t *testing.T) {
	t.Parallel()

	past := conv.ToPGTimestamptz(time.Now().Add(-time.Minute))
	future := conv.ToPGTimestamptz(time.Now().Add(time.Hour))
	none := pgtype.Timestamptz{}
	for _, tc := range []struct {
		name                           string
		access, refresh, authorization pgtype.Timestamptz
	}{
		{name: "authorization ended", access: future, refresh: none, authorization: past},
		{name: "refresh ended with access expired", access: past, refresh: past, authorization: none},
		{name: "authorization ended with refresh live", access: past, refresh: future, authorization: past},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tokenCalls atomic.Int64
			ctx, env := newSyntheticExpiryEnv(t, "genuine-"+uniqueSuffix(tc.name), func(w http.ResponseWriter, _ *http.Request) {
				tokenCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"live-access","refresh_token":"live-refresh","token_type":"Bearer"}`))
			})
			storeGrantDeadlines(t, ctx, env, tc.access, tc.refresh, tc.authorization)
			exchanged := tokenCalls.Load()

			require.Equal(t, remotesessions.RemoteSessionExpired, sessionStatus(t, ctx, env))
			requireNoToken(t, ctx, env)
			require.Equal(t, exchanged, tokenCalls.Load())
		})
	}
}

// A row stored dead by the old callback recovers on reconnect, even when the
// provider still reports the same zero lifetime.
func TestReconnectRecoversGrantStoredDead(t *testing.T) {
	t.Parallel()

	stored := conv.ToPGTimestamptz(time.Now().Add(-time.Hour))
	none := pgtype.Timestamptz{}
	for _, tc := range []struct {
		name                           string
		access, refresh, authorization pgtype.Timestamptz
	}{
		{name: "authorization stored dead", access: none, refresh: none, authorization: stored},
		{name: "refresh stored dead", access: stored, refresh: stored, authorization: none},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, env := newSyntheticExpiryEnv(t, "reconnect-dead-"+uniqueSuffix(tc.name), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"reconnected-access","refresh_token":"live-refresh","token_type":"Bearer","authorization_expires_in":0,"refresh_token_timeout":0}`))
			})
			storeGrantDeadlines(t, ctx, env, tc.access, tc.refresh, tc.authorization)
			require.Equal(t, remotesessions.RemoteSessionExpired, sessionStatus(t, ctx, env))
			requireNoToken(t, ctx, env)

			row := reconnectSyntheticLogin(t, ctx, env)

			require.False(t, row.AuthorizationExpiresAt.Valid)
			require.False(t, row.RefreshExpiresAt.Valid)
			require.Equal(t, remotesessions.RemoteSessionActive, sessionStatus(t, ctx, env))
			token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
			require.NoError(t, err)
			require.Equal(t, "reconnected-access", token)
		})
	}
}

// A refresh, on the request path or the background sweep, handles lifetimes the way the callback does.
func TestRefreshHandlesNonPositiveLifetimes(t *testing.T) {
	t.Parallel()

	for _, trigger := range []remotesessionmetrics.RefreshTrigger{remotesessionmetrics.RefreshTriggerRequest, remotesessionmetrics.RefreshTriggerScheduled} {
		for _, tc := range []struct {
			name              string
			fields            string
			keepAuthorization bool
			keepRefresh       bool
			refreshDeadline   time.Duration
			authorization     time.Duration
		}{
			{name: "zero authorization keeps the known deadline", fields: `"refresh_token":"rotated","authorization_expires_in":0`, keepAuthorization: true},
			{name: "negative authorization keeps the known deadline", fields: `"refresh_token":"rotated","authorization_expires_in":-1`, keepAuthorization: true},
			{name: "zero timeout on a rotated token stores none", fields: `"refresh_token":"rotated","refresh_token_timeout":0`, keepAuthorization: true},
			{name: "negative timeout on a rotated token stores none", fields: `"refresh_token":"rotated","refresh_token_timeout":-1`, keepAuthorization: true},
			{name: "zero timeout on the same token keeps the known deadline", fields: `"refresh_token":"live-refresh","refresh_token_timeout":0`, keepAuthorization: true, keepRefresh: true},
			{name: "positive lifetimes replace", fields: `"refresh_token":"rotated","authorization_expires_in":600,"refresh_token_timeout":300`, authorization: 10 * time.Minute, refreshDeadline: 5 * time.Minute},
		} {
			t.Run(string(trigger)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				ctx, env := newSyntheticExpiryEnv(t, "refresh-life-"+uniqueSuffix(string(trigger)+tc.name), func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if err := r.ParseForm(); err == nil && r.PostForm.Get("grant_type") == "refresh_token" {
						_, _ = fmt.Fprintf(w, `{"access_token":"refreshed","token_type":"Bearer","expires_in":3600,%s}`, tc.fields)
						return
					}
					_, _ = w.Write([]byte(`{"access_token":"first","refresh_token":"live-refresh","token_type":"Bearer","expires_in":3600,"authorization_expires_in":86400,"refresh_token_timeout":7200}`))
				})
				known, knownRefresh := env.session.AuthorizationExpiresAt.Time, env.session.RefreshExpiresAt.Time

				_, err := env.refresher.RefreshNow(ctx, env.session, "", trigger)
				require.NoError(t, err)

				row := activeSession(t, ctx, env)
				plain, err := env.enc.Decrypt(row.AccessTokenEncrypted)
				require.NoError(t, err)
				require.Equal(t, "refreshed", plain)
				if tc.keepAuthorization {
					require.WithinDuration(t, known, row.AuthorizationExpiresAt.Time, time.Second)
				} else {
					require.WithinDuration(t, time.Now().Add(tc.authorization), row.AuthorizationExpiresAt.Time, 30*time.Second)
				}
				switch {
				case tc.keepRefresh:
					require.WithinDuration(t, knownRefresh, row.RefreshExpiresAt.Time, time.Second)
				case tc.refreshDeadline == 0:
					require.False(t, row.RefreshExpiresAt.Valid)
				default:
					require.WithinDuration(t, time.Now().Add(tc.refreshDeadline), row.RefreshExpiresAt.Time, 30*time.Second)
				}
				require.Equal(t, remotesessions.RemoteSessionActive, sessionStatus(t, ctx, env))
			})
		}
	}
}

// uniqueSuffix turns a case name into a slug-safe suffix.
func uniqueSuffix(name string) string {
	var hash uint32 = 2166136261
	for _, b := range []byte(name) {
		hash = (hash ^ uint32(b)) * 16777619
	}
	return fmt.Sprintf("%08x", hash)
}
