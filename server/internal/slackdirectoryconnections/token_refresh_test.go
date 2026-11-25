package slackdirectoryconnections_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

type refresherFunc func(context.Context, string) (*slackdirectoryconnections.TokenBundle, error)

func (f refresherFunc) Refresh(ctx context.Context, token string) (*slackdirectoryconnections.TokenBundle, error) {
	return f(ctx, token)
}

func storeTokens(t *testing.T, ctx context.Context, f *fixture, c *gen.SlackDirectoryConnection, expiresAt time.Time) {
	t.Helper()
	plaintext, err := json.Marshal(slackdirectoryconnections.TokenBundle{Version: 1, AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expiresAt, TokenType: "bot"})
	require.NoError(t, err)
	ciphertext, err := f.enc.Encrypt(plaintext)
	require.NoError(t, err)
	updated, err := repo.New(f.db).UpdateSlackDirectoryCredentials(ctx, repo.UpdateSlackDirectoryCredentialsParams{CredentialsEncrypted: conv.ToPGText(ciphertext), OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(c.ID), Generation: uuid.MustParse(c.Generation)})
	require.NoError(t, err)
	require.EqualValues(t, 1, updated)
}

func storedTokens(t *testing.T, ctx context.Context, f *fixture, c *gen.SlackDirectoryConnection) slackdirectoryconnections.TokenBundle {
	t.Helper()
	row, err := repo.New(f.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(c.ID)})
	require.NoError(t, err)
	plaintext, err := f.enc.Decrypt(row.CredentialsEncrypted.String)
	require.NoError(t, err)
	var tokens slackdirectoryconnections.TokenBundle
	require.NoError(t, json.Unmarshal([]byte(plaintext), &tokens))
	return tokens
}

func tokenCheckingSnapshot(t *testing.T, want string) directoryFunc {
	t.Helper()
	return func(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		require.Equal(t, want, token)
		return snapshot("UEXAMPLE01")(ctx, token, team, report)
	}
}

func TestSyncRefreshesAnExpiringTokenAndStoresItFirst(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	storeTokens(t, ctx, f, c, time.Now().Add(5*time.Minute))
	renewed := time.Now().Add(12 * time.Hour).UTC().Truncate(time.Second)
	refresher := refresherFunc(func(_ context.Context, token string) (*slackdirectoryconnections.TokenBundle, error) {
		require.Equal(t, "old-refresh", token)
		return &slackdirectoryconnections.TokenBundle{Version: 1, AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: &renewed, TokenType: "bot"}, nil
	})
	worker := slackdirectoryconnections.NewDirectorySync(f.db, f.enc, tokenCheckingSnapshot(t, "new-access"), audit.NewLogger(), refresher)
	require.NoError(t, worker.Run(ctx, syncRequest(f, c), nil))
	stored := storedTokens(t, ctx, f, c)
	require.Equal(t, "new-access", stored.AccessToken)
	require.Equal(t, "new-refresh", stored.RefreshToken)
	require.Len(t, members(t, ctx, f), 1)
}

func TestSyncLeavesAFreshTokenAlone(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	storeTokens(t, ctx, f, c, time.Now().Add(2*time.Hour))
	refresher := refresherFunc(func(context.Context, string) (*slackdirectoryconnections.TokenBundle, error) {
		t.Error("a fresh token was refreshed")
		return nil, errors.New("unexpected refresh")
	})
	worker := slackdirectoryconnections.NewDirectorySync(f.db, f.enc, tokenCheckingSnapshot(t, "old-access"), audit.NewLogger(), refresher)
	require.NoError(t, worker.Run(ctx, syncRequest(f, c), nil))
	require.Equal(t, "old-access", storedTokens(t, ctx, f, c).AccessToken)
}

func TestSyncRefreshFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		err        error
		code       string
		retryable  bool
		retryAfter time.Duration
	}{
		{name: "rejected", err: &slackdirectoryconnections.ProviderError{Code: "invalid_grant", RetryAfter: 0}, code: "authorization_expired", retryable: false, retryAfter: 0},
		{name: "transient", err: &slackdirectoryconnections.ProviderError{Code: "transport", RetryAfter: 0}, code: "refresh_unavailable", retryable: true, retryAfter: 0},
		{name: "rate limited", err: &slackdirectoryconnections.ProviderError{Code: "rate_limited", RetryAfter: 7 * time.Minute}, code: "rate_limited", retryable: true, retryAfter: 7 * time.Minute},
		{name: "rate limited without delay", err: &slackdirectoryconnections.ProviderError{Code: "ratelimited", RetryAfter: 0}, code: "rate_limited", retryable: true, retryAfter: time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newService(t)
			c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
			storeTokens(t, ctx, f, c, time.Now().Add(-time.Minute))
			refresher := refresherFunc(func(context.Context, string) (*slackdirectoryconnections.TokenBundle, error) { return nil, tc.err })
			fetch := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
				t.Error("fetched with an unusable token")
				return nil, nil
			})
			err := slackdirectoryconnections.NewDirectorySync(f.db, f.enc, fetch, audit.NewLogger(), refresher).Run(ctx, syncRequest(f, c), nil)
			var syncErr *slackdirectoryconnections.SyncError
			require.ErrorAs(t, err, &syncErr)
			require.Equal(t, tc.code, syncErr.Code)
			require.Equal(t, tc.retryable, syncErr.Retryable)
			require.Equal(t, tc.retryAfter, syncErr.RetryAfter)
			require.Equal(t, "old-refresh", storedTokens(t, ctx, f, c).RefreshToken)
		})
	}
}

func TestExpiredRefreshableTokenStillAllowsManualSync(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	storeTokens(t, ctx, f, c, time.Now().Add(-time.Hour))
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "connected", list.Connections[0].Status)
	result, err := f.service.Sync(ctx, &gen.SyncPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.NoError(t, err)
	require.True(t, result.Accepted)
}

func TestScheduledSyncIsNotAudited(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	input.ActorID = ""
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, input, nil))
	require.Len(t, members(t, ctx, f), 1)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionSync)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestProviderRefreshRotatesTokens(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body map[string]any
		ok   bool
	}{
		{name: "rotated", body: map[string]any{"ok": true, "access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 43200, "token_type": "bot"}, ok: true},
		{name: "missing refresh token", body: map[string]any{"ok": true, "access_token": "new-access", "expires_in": 43200, "token_type": "bot"}, ok: false},
		{name: "rejected", body: map[string]any{"ok": false, "error": "invalid_refresh_token"}, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			type seen struct {
				grant, token, client string
				basic                bool
			}
			requests := make(chan seen, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, _, ok := r.BasicAuth()
				_ = r.ParseForm()
				requests <- seen{grant: r.Form.Get("grant_type"), token: r.Form.Get("refresh_token"), client: id, basic: ok}
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			t.Cleanup(server.Close)
			p := slackdirectoryconnections.NewOAuthProvider(slackapi.NewClient(server.URL, server.Client()), "synthetic-client", "synthetic-secret", "")
			tokens, err := p.Refresh(t.Context(), "old-refresh")
			request := <-requests
			require.Equal(t, "refresh_token", request.grant)
			require.Equal(t, "old-refresh", request.token)
			require.True(t, request.basic)
			require.Equal(t, "synthetic-client", request.client)
			if !tc.ok {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "new-access", tokens.AccessToken)
			require.Equal(t, "new-refresh", tokens.RefreshToken)
			require.NotNil(t, tokens.ExpiresAt)
			require.WithinDuration(t, time.Now().Add(12*time.Hour), *tokens.ExpiresAt, time.Minute)
		})
	}
}

func TestProviderRefreshRateLimitKeepsRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "420")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	p := slackdirectoryconnections.NewOAuthProvider(slackapi.NewClient(server.URL, server.Client()), "synthetic-client", "synthetic-secret", "")
	_, err := p.Refresh(t.Context(), "old-refresh")
	var providerErr *slackdirectoryconnections.ProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "rate_limited", providerErr.Code)
	require.Equal(t, 7*time.Minute, providerErr.RetryAfter)
}

func TestSweepBacksOffAFailingWorkspace(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	tooLarge := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		return nil, &slackdirectoryconnections.SyncError{Code: "directory_too_large", Retryable: false, Reconnect: false, RetryAfter: 0}
	})
	require.Error(t, syncer(f, tooLarge).Run(ctx, syncRequest(f, c), nil))
	due := func(failedAfter time.Time) int {
		rows, err := repo.New(f.db).ListDueSlackDirectorySyncs(ctx, repo.ListDueSlackDirectorySyncsParams{
			ExcludedOrganizationID: "org_synthetic_excluded",
			StartedBefore:          pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
			FailedAfter:            pgtype.Timestamptz{Time: failedAfter, Valid: true, InfinityModifier: pgtype.Finite},
			MaxRows:                100,
		})
		require.NoError(t, err)
		n := 0
		for _, row := range rows {
			if row.ID.String() == c.ID {
				n++
			}
		}
		return n
	}
	require.Zero(t, due(time.Now().Add(-6*time.Hour)), "a recent failure is backed off")
	require.Equal(t, 1, due(time.Now().Add(time.Hour)), "an older failure is retried")
}
