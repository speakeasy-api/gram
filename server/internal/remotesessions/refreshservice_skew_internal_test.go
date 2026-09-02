package remotesessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestAccessTokenUsable_DeadlineInsideSkewWithRefreshIsExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt:       conv.ToPGTimestamptz(now.Add(AccessTokenExpirySkew / 2)),
		RefreshTokenEncrypted: conv.ToPGText("enc-refresh"),
	}
	require.False(t, accessTokenUsable(sess, now), "a token expiring inside the skew window must be refreshed, not forwarded")
}

func TestAccessTokenUsable_DeadlineInsideSkewWithoutRefreshIsUsable(t *testing.T) {
	t.Parallel()

	// With nothing to refresh with, the only alternative to forwarding is a
	// reconnect prompt AccessTokenExpirySkew early — while the consent UI still
	// reports the session as connected.
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt: conv.ToPGTimestamptz(now.Add(AccessTokenExpirySkew / 2)),
	}
	require.True(t, accessTokenUsable(sess, now), "without a refresh grant the token is forwarded until its stated deadline")
}

func TestAccessTokenUsable_DeadlineInsideSkewWithExpiredRefreshIsUsable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt:       conv.ToPGTimestamptz(now.Add(AccessTokenExpirySkew / 2)),
		RefreshTokenEncrypted: conv.ToPGText("enc-refresh"),
		RefreshExpiresAt:      conv.ToPGTimestamptz(now.Add(-time.Second)),
	}
	require.True(t, accessTokenUsable(sess, now), "a refresh grant past its idle timeout is no refresh path; the token is forwarded until its deadline")
}

func TestAccessTokenUsable_PastDeadlineWithoutRefreshIsUnusable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt: conv.ToPGTimestamptz(now.Add(-time.Second)),
	}
	require.False(t, accessTokenUsable(sess, now))
}

func TestAccessTokenUsable_DeadlineBeyondSkewIsUsable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt:       conv.ToPGTimestamptz(now.Add(AccessTokenExpirySkew + time.Second)),
		RefreshTokenEncrypted: conv.ToPGText("enc-refresh"),
	}
	require.True(t, accessTokenUsable(sess, now))
}

func TestAccessTokenUsable_NoKnownExpiryIsUsable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	require.True(t, accessTokenUsable(remotesessions_repo.RemoteSession{}, now))
}

func TestAccessTokenUsable_ExpiredAuthorizationIsUnusable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt:        conv.ToPGTimestamptz(now.Add(24 * time.Hour)),
		AuthorizationExpiresAt: conv.ToPGTimestamptz(now.Add(-time.Second)),
	}
	require.False(t, accessTokenUsable(sess, now))
}

func TestAccessTokenLive_DeadlineInsideSkewIsLive(t *testing.T) {
	t.Parallel()

	// The adoption bar for a row a concurrent refresh just wrote ignores the
	// skew: that token is the newest a refresh can produce.
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt:       conv.ToPGTimestamptz(now.Add(AccessTokenExpirySkew / 2)),
		RefreshTokenEncrypted: conv.ToPGText("enc-refresh"),
	}
	require.True(t, accessTokenLive(sess, now))
	require.False(t, accessTokenUsable(sess, now), "the same row still triggers a refresh when read from a stale snapshot")
}

func TestAccessTokenLive_PastDeadlineIsNotLive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt: conv.ToPGTimestamptz(now.Add(-time.Second)),
	}
	require.False(t, accessTokenLive(sess, now))
}

func TestAccessTokenLive_ExpiredAuthorizationIsNotLive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt:        conv.ToPGTimestamptz(now.Add(24 * time.Hour)),
		AuthorizationExpiresAt: conv.ToPGTimestamptz(now.Add(-time.Second)),
	}
	require.False(t, accessTokenLive(sess, now))
}
