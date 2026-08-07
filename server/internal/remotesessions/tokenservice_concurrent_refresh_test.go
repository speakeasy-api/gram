// Reproduces the production failure behind repeated "please reconnect" reports
// on issuer-gated MCP servers. Refreshing is a read-modify-write around an
// external POST, so unserialized, concurrent resolves for one subject all
// present the same stored refresh token. A provider that rotates single-use
// tokens honours the first and rejects the rest, stranding the losers even
// though the winner has already repaired the row.

package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Any value above one exercises the race; eight makes it near-certain.
const concurrentRefreshCallers = 8

const initialRefreshToken = "refresh-initial"

// Releases the gate when no further callers are arriving, which post-fix is
// always. Must stay well under the losers' wait budget, or they give up on the
// lock holder and issue the duplicate POST the fix exists to prevent.
const refreshGateSettle = 300 * time.Millisecond

// rotatingUpstream models a provider that rotates refresh tokens on use and
// rejects a consumed one, with the verbatim 401 body seen in production.
// Requests are gated until every caller arrives so the overlap is deterministic.
type rotatingUpstream struct {
	mu        sync.Mutex
	live      map[string]bool
	rotations int
	attempts  int
	replays   int
	arrived   int
	gate      chan struct{}
}

func newRotatingUpstream() *rotatingUpstream {
	return &rotatingUpstream{
		mu:        sync.Mutex{},
		live:      map[string]bool{initialRefreshToken: true},
		rotations: 0,
		attempts:  0,
		replays:   0,
		arrived:   0,
		gate:      make(chan struct{}),
	}
}

// refreshAttempts reports how many refresh_token grants reached the upstream.
func (u *rotatingUpstream) refreshAttempts() int {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.attempts
}

// consumedReplays reports how many grants presented an already-rotated refresh
// token. This mock answers one with a 401, which Gram recovers from, but a
// provider implementing reuse detection revokes the whole token family instead,
// killing the winner's fresh pair with no local symptom.
func (u *rotatingUpstream) consumedReplays() int {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.replays
}

func (u *rotatingUpstream) handler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "application/json")

	if r.Form.Get("grant_type") != "refresh_token" {
		// Initial exchange. Tests mark the resulting access token expired
		// directly so concurrent requests all exercise lazy refresh.
		_, _ = w.Write([]byte(`{"access_token":"access-initial","refresh_token":"` + initialRefreshToken + `","token_type":"Bearer"}`))

		return
	}

	u.mu.Lock()
	u.attempts++
	u.arrived++
	if u.arrived == concurrentRefreshCallers {
		close(u.gate)
	}
	u.mu.Unlock()

	select {
	case <-u.gate:
	case <-time.After(refreshGateSettle):
	}

	presented := r.Form.Get("refresh_token")

	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.live[presented] {
		u.replays++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Refresh token not found.","doc_url":"https://example.test/docs/errors#unauthorized"}}`))

		return
	}

	delete(u.live, presented)
	u.rotations++
	rotated := fmt.Sprintf("refresh-rotated-%d", u.rotations)
	u.live[rotated] = true

	_, _ = w.Write(fmt.Appendf(nil,
		`{"access_token":"access-rotated-%d","refresh_token":%q,"token_type":"Bearer"}`,
		u.rotations, rotated))
}

// resolveConcurrently drives concurrentRefreshCallers simultaneous resolves
// against a session whose upstream token has aged out.
func resolveConcurrently(t *testing.T, slugSuffix string, upstream *rotatingUpstream) (context.Context, syntheticExpiryEnv, []string) {
	t.Helper()

	ctx, env := newSyntheticExpiryEnv(t, slugSuffix, upstream.handler)

	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
	}))

	tokens := make([]string, concurrentRefreshCallers)
	errs := make([]error, concurrentRefreshCallers)

	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := range concurrentRefreshCallers {
		wg.Go(func() {
			<-start
			tokens[i], errs[i] = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
		})
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "caller %d returned an unexpected error", i)
	}

	return ctx, env, tokens
}

// The correctness guard: losing a refresh race must not strand a caller, since
// the winner's rotation has already repaired the row for everyone.
func TestResolveAccessToken_ConcurrentRefresh_AllCallersGetUsableToken(t *testing.T) {
	t.Parallel()

	upstream := newRotatingUpstream()
	ctx, env, tokens := resolveConcurrently(t, "concurrent-refresh", upstream)

	// An empty token becomes ErrNoValidToken, then a 401 at the MCP gate, then
	// one user being told to reconnect.
	stranded := 0

	for _, tok := range tokens {
		if tok == "" {
			stranded++
		}
	}

	// Asserted before the count so it shows up even on a failing run: every
	// stranded caller was turned away from a session usable the whole time.
	repaired, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.NotEmpty(t, repaired, "the winning rotation should have left the session usable")

	require.Zero(t, stranded,
		"%d of %d concurrent callers resolved no token; each one is a user-visible reconnect prompt",
		stranded, concurrentRefreshCallers)

	// Recovering from a replay is not the same as avoiding one: against a
	// reuse-detecting provider the replay is what breaks the session.
	require.Zero(t, upstream.consumedReplays(),
		"a consumed refresh token was presented upstream %d time(s)",
		upstream.consumedReplays())
}

// The efficiency guard: without it a broken session drives a sustained request
// storm against the provider.
func TestResolveAccessToken_ConcurrentRefresh_CollapsesToSingleUpstreamCall(t *testing.T) {
	t.Parallel()

	upstream := newRotatingUpstream()
	_, _, _ = resolveConcurrently(t, "concurrent-refresh-single", upstream)

	require.Equal(t, 1, upstream.refreshAttempts(),
		"concurrent resolves for one subject must collapse into a single upstream refresh")
}
