// tokenservice_revoke_during_refresh_test.go guards the security property that
// revoking a remote session is final: an MCP request already mid-refresh must
// not write the rotated tokens back and resurrect the session.
//
// The refresh path is a read-modify-write around an external POST. While that
// POST is in flight an admin can soft-delete the row, which drops it out of the
// partial unique index the write targets. A write that can insert would then
// undo the revocation; an update-only write cannot.

package remotesessions_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestResolveAccessToken_RevokedDuringRefresh_StaysRevoked(t *testing.T) {
	t.Parallel()

	refreshArrived := make(chan struct{})
	releaseRefresh := make(chan struct{})

	ctx, env := newSyntheticExpiryEnv(t, "revoke-during-refresh", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")

		if r.Form.Get("grant_type") != "refresh_token" {
			// Initial exchange: no expires_in but a refresh token, so the row
			// lands in the NULL-expiry case whose window follows updated_at.
			_, _ = w.Write([]byte(`{"access_token":"access-initial","refresh_token":"refresh-initial","token_type":"Bearer"}`))

			return
		}

		close(refreshArrived)
		<-releaseRefresh

		_, _ = w.Write([]byte(`{"access_token":"access-rotated","refresh_token":"refresh-rotated","token_type":"Bearer"}`))
	})

	// Push updated_at outside the refresh cadence so the next resolve must
	// refresh rather than serve the stored token.
	require.NoError(t, env.q.SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        env.session.ID,
		ProjectID: conv.ToNullUUID(env.projectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-2 * time.Hour)),
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

	revoked, err := env.q.RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{
		ID:        env.session.ID,
		ProjectID: env.projectID,
	})
	require.NoError(t, err)
	require.True(t, revoked.Deleted, "the session must be soft-deleted before the refresh completes")

	close(releaseRefresh)

	got := <-resolved
	require.NoError(t, got.err, "a revoked session is not an error, just no usable token")
	require.Empty(t, got.token, "a revoked session must resolve to no token")

	_, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the in-flight refresh resurrected a revoked session")
}
