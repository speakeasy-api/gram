// The scheduled sweep's correctness guard: a RefreshService caller (the
// Temporal activity) racing lazy MCP resolves for the same session must
// collapse into one upstream refresh_token grant. Anything else presents a
// consumed refresh token to providers with rotation reuse detection, which
// revokes the whole token family.

package remotesessions_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRefreshNow_RacingLazyResolves_SingleUpstreamCall(t *testing.T) {
	t.Parallel()

	upstream := newRotatingUpstream()
	ctx, env := newSyntheticExpiryEnv(t, "sched-vs-lazy", upstream.handler)

	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
	}))

	// Re-read so the scheduled caller starts from the backdated row, exactly
	// as the activity's candidate query would.
	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	const scheduled = 2
	lazy := concurrentRefreshCallers - scheduled

	tokens := make([]string, concurrentRefreshCallers)
	errs := make([]error, concurrentRefreshCallers)
	outcomes := make([]remotesessions.RefreshOutcome, scheduled)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range lazy {
		wg.Go(func() {
			<-start
			tokens[i], errs[i] = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
		})
	}
	for i := range scheduled {
		wg.Go(func() {
			<-start
			result, err := env.refresher.RefreshNow(ctx, session, "")
			tokens[lazy+i], errs[lazy+i] = result.AccessToken, err
			outcomes[i] = result.Outcome
		})
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "caller %d returned an unexpected error", i)
	}
	for i, tok := range tokens {
		require.NotEmpty(t, tok, "caller %d resolved no token", i)
	}
	for i, outcome := range outcomes {
		require.Contains(t,
			[]remotesessions.RefreshOutcome{remotesessions.RefreshOutcomeRefreshed, remotesessions.RefreshOutcomeAdoptedConcurrentWinner},
			outcome, "scheduled caller %d reported an unexpected outcome", i)
	}

	require.Equal(t, 1, upstream.refreshAttempts(),
		"scheduled and lazy callers for one session must collapse into a single upstream refresh")
	require.Zero(t, upstream.consumedReplays(),
		"a consumed refresh token was presented upstream %d time(s)", upstream.consumedReplays())
}
