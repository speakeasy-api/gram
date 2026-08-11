package remotesessions_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestResolveAccessToken_InvalidGrantClearsRefreshGrant(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "invalid-grant-eviction", func(w http.ResponseWriter, r *http.Request) {
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

	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
	}))

	token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)

	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.False(t, active.RefreshTokenEncrypted.Valid)
	require.False(t, active.RefreshExpiresAt.Valid)

	// A second MCP request no longer retries the dead upstream token.
	token, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)
	require.EqualValues(t, 1, refreshAttempts.Load())

	// The next upstream OAuth callback repairs the existing row in place and
	// preserves its per-session preferences.
	freshAccess, err := testenv.NewEncryptionClient(t).Encrypt([]byte("fresh-access"))
	require.NoError(t, err)
	fresh, err := env.q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            env.subject,
		UserSessionIssuerID:   env.session.UserSessionIssuerID,
		RemoteSessionClientID: env.clientID,
		AccessTokenEncrypted:  freshAccess,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: pgtype.Text{},
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                env.session.Scopes,
		Resource:              pgtype.Text{},
	})
	require.NoError(t, err)
	require.Equal(t, env.session.ID, fresh.ID)
}

// TestResolveAccessToken_InvalidGrantAdoptsConcurrentRelink covers the CAS
// fall-through: when a concurrent winner (e.g. a fresh OAuth callback) rotates
// the row while the lock owner is mid-POST, the owner's delayed invalid_grant
// must adopt the winner's token rather than evict the freshly re-linked session.
func TestResolveAccessToken_InvalidGrantAdoptsConcurrentRelink(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	refreshArrived := make(chan struct{})
	releaseRefresh := make(chan struct{})

	// Close releaseRefresh exactly once, and register a cleanup so an assertion
	// failure between <-refreshArrived and the happy-path close still unblocks
	// the blocked handler goroutine instead of leaking it forever.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRefresh) }) }
	t.Cleanup(release)

	ctx, env := newSyntheticExpiryEnv(t, "invalid-grant-adopt", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"access-initial","refresh_token":"refresh-initial"}`))
			return
		}

		// Block mid-refresh so the test can land a concurrent re-link that
		// rotates updated_at before this POST reports invalid_grant.
		refreshAttempts.Add(1)
		close(refreshArrived)
		<-releaseRefresh

		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	})

	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
	}))

	type resolveResult struct {
		token string
		err   error
	}
	resolved := make(chan resolveResult, 1)
	go func() {
		tok, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
		resolved <- resolveResult{token: tok, err: err}
	}()

	<-refreshArrived

	// The winner rotates the row's tokens and updated_at in place, invalidating
	// the owner's compare-and-swap snapshot.
	enc := testenv.NewEncryptionClient(t)
	relinkedAccess, err := enc.Encrypt([]byte("access-relinked"))
	require.NoError(t, err)
	relinkedRefresh, err := enc.Encrypt([]byte("refresh-relinked"))
	require.NoError(t, err)
	winner, err := env.q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            env.subject,
		UserSessionIssuerID:   env.session.UserSessionIssuerID,
		RemoteSessionClientID: env.clientID,
		AccessTokenEncrypted:  relinkedAccess,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText(relinkedRefresh),
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                env.session.Scopes,
		Resource:              pgtype.Text{},
	})
	require.NoError(t, err)
	require.Equal(t, env.session.ID, winner.ID, "re-link updates the row in place")

	release()

	got := <-resolved
	require.NoError(t, got.err)
	require.Equal(t, "access-relinked", got.token, "the owner adopts the winner's token instead of evicting")

	// The row survives: a stale invalid_grant must not evict a re-linked session.
	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, env.session.ID, active.ID)
	require.False(t, active.Deleted)

	require.EqualValues(t, 1, refreshAttempts.Load(), "only the lock owner attempts the upstream refresh")
}
