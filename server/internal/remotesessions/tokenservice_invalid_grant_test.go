package remotesessions_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestResolveAccessToken_InvalidGrantRevokesDeadSession(t *testing.T) {
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

	require.NoError(t, env.q.SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        env.session.ID,
		ProjectID: conv.ToNullUUID(env.projectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-2 * time.Hour)),
	}))

	token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)

	_, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// A second MCP request no longer retries the dead upstream token.
	token, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)
	require.EqualValues(t, 1, refreshAttempts.Load())

	// The next upstream OAuth callback can create a fresh active session because
	// the dead row no longer participates in the partial unique index.
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
	})
	require.NoError(t, err)
	require.NotEqual(t, env.session.ID, fresh.ID)
}
