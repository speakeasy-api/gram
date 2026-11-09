// The scheduled sweep's correctness guard: a RefreshService caller (the
// Temporal activity) racing lazy MCP resolves for the same session must
// collapse into one upstream refresh_token grant. Anything else presents a
// consumed refresh token to providers with rotation reuse detection, which
// revokes the whole token family.

package remotesessions_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
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
	outcomes := make([]remotesessionmetrics.RefreshOutcome, scheduled)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range lazy {
		wg.Go(func() {
			<-start
			tokens[i], errs[i] = env.mgr.ResolveAccessToken(ctx, remotesessions.RefreshAttribution{}, env.clientID, env.subject, "")
		})
	}
	for i := range scheduled {
		wg.Go(func() {
			<-start
			result, err := env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerScheduled, remotesessions.RefreshAttribution{})
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
			[]remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeRefreshed, remotesessionmetrics.RefreshOutcomeAdoptedConcurrentWinner},
			outcome, "scheduled caller %d reported an unexpected outcome", i)
	}

	require.Equal(t, 1, upstream.refreshAttempts(),
		"scheduled and lazy callers for one session must collapse into a single upstream refresh")
	require.Zero(t, upstream.consumedReplays(),
		"a consumed refresh token was presented upstream %d time(s)", upstream.consumedReplays())
}

func TestRefreshNow_SiblingWaiterReacquiresSharedLock(t *testing.T) {
	t.Parallel()
	const resourceA = "https://a.example/mcp"
	const resourceB = "https://b.example/mcp"
	upstream := newRotatingUpstream()
	entered := make(chan string, 2)
	releaseA := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseA) }) }
	defer release()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			upstream.handler(w, r)
			return
		}
		resource := r.Form.Get("resource")
		entered <- resource
		if resource == resourceA {
			select {
			case <-releaseA:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":"rotated"}`, resource)
	}
	ctx, env := newSyntheticExpiryEnv(t, "sibling-refresh-waiter", handler)
	expandResourceGrantIndexes(t, ctx, env.db)
	params := repo.UpsertRemoteSessionParams{
		SubjectUrn: env.subject, UserSessionIssuerID: env.session.UserSessionIssuerID, RemoteSessionClientID: env.clientID,
		AccessTokenEncrypted: env.session.AccessTokenEncrypted, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
		RefreshTokenEncrypted: env.session.RefreshTokenEncrypted, Scopes: env.session.Scopes,
		Resource: conv.ToPGText(resourceA),
	}
	a, err := env.q.UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
	params.Resource = conv.ToPGText(resourceB)
	b, err := env.q.UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
	type result struct {
		value remotesessions.RefreshResult
		err   error
	}
	doneA := make(chan result, 1)
	doneB := make(chan result, 1)
	refresh := func(row repo.RemoteSession, resource string, done chan<- result) {
		value, err := env.refresher.RefreshNow(ctx, row, resource, remotesessionmetrics.RefreshTriggerScheduled, remotesessions.RefreshAttribution{})
		done <- result{value, err}
	}
	go refresh(a, resourceA, doneA)
	select {
	case resource := <-entered:
		require.Equal(t, resourceA, resource)
	case <-time.After(5 * time.Second):
		t.Fatal("first refresh never started")
	}
	go refresh(b, resourceB, doneB)
	select {
	case resource := <-entered:
		t.Fatalf("shared lock allowed overlapping refresh for %s", resource)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case got := <-doneA:
		require.NoError(t, got.err)
		require.Equal(t, a.ID, got.value.Session.ID)
		require.Equal(t, resourceA, got.value.AccessToken)
	case <-time.After(5 * time.Second):
		t.Fatal("first refresh did not finish")
	}
	select {
	case got := <-doneB:
		require.NoError(t, got.err)
		require.Equal(t, b.ID, got.value.Session.ID)
		require.Equal(t, resourceB, got.value.AccessToken)
		require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, got.value.Outcome)
	case <-time.After(3 * time.Second):
		t.Fatal("sibling waiter did not reacquire the released lock promptly")
	}
	require.Equal(t, resourceB, <-entered)
}
