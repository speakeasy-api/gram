package remotesessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestAccessTokenUsable_DeadlineInsideSkewIsExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt: conv.ToPGTimestamptz(now.Add(accessTokenExpirySkew / 2)),
	}
	require.False(t, accessTokenUsable(sess, now), "a token expiring inside the skew window must be refreshed, not forwarded")
}

func TestAccessTokenUsable_DeadlineBeyondSkewIsUsable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sess := remotesessions_repo.RemoteSession{
		AccessExpiresAt: conv.ToPGTimestamptz(now.Add(accessTokenExpirySkew + time.Second)),
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
