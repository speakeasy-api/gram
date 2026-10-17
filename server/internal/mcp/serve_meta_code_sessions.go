package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// codeSessionIdleTTL keeps a member session for a caller's next execution
	// in a burst; the cache serves rapid successive requests, not long idles.
	codeSessionIdleTTL = 5 * time.Minute
	// codeSessionMaxAge stays below the live-grant witness window, so a
	// session's listing still authorizes its calls.
	codeSessionMaxAge = 10 * time.Minute
	// codeSessionPingAfter is the idle time after which a reused session is
	// pinged before any call; a burst within it pays a single round trip.
	codeSessionPingAfter = 30 * time.Second
	// codeSessionMaxEntries bounds the sessions one pod keeps open.
	codeSessionMaxEntries = 4096
)

// codeMemberSessionsKey carries an execution's *codeSessionScope to member dispatch.
type codeMemberSessionsKey struct{}

// codeSessionScope is one execution's view of a member session pool: the
// pod's shared pool under the verified caller's prefix, or a pool of its own
// for an anonymous caller.
type codeSessionScope struct {
	pool   *codeMemberSessions
	prefix string
	// affinity replaces a per-request caller identity in tunnel routing, so
	// a reused session keeps landing on the tunnel gateway that holds it.
	affinity string
	// recheck re-runs the execution's admission for an operation that
	// waited on another, returning the refreshed authority to dispatch with,
	// so a revocation during the wait still applies.
	recheck func(ctx context.Context, slug string) (context.Context, error)
	// owned pools close when their execution ends.
	owned bool
}

func (s *codeSessionScope) end(ctx context.Context) {
	if s.owned {
		s.pool.close(ctx, false)
	}
}

// codeCallerIdentity is the caller key member routing pins tunnel affinity on.
func codeCallerIdentity(ctx context.Context, gate *metaGateContext) string {
	if scope, _ := ctx.Value(codeMemberSessionsKey{}).(*codeSessionScope); scope != nil && scope.affinity != "" {
		return scope.affinity
	}
	return gate.callerIdentity()
}

// codeMemberSessions holds open proxied member sessions so a member's later
// Code Mode operations skip the MCP handshake. A session never crosses
// callers, grants, routes or credentials: those are all in its key. Every
// operation still runs its own admission, routes, authorizes and rebuilds
// its proxy; the session carries no authority of its own.
type codeMemberSessions struct {
	logger *slog.Logger
	// idleTTL and maxAge of zero keep sessions until close.
	idleTTL time.Duration
	maxAge  time.Duration
	// pingAfter is how long a session may sit idle before a reuse first
	// pings it, so a session the member expired is replaced before any call.
	pingAfter  time.Duration
	maxEntries int

	mu      sync.Mutex
	closed  bool
	entries map[string]*codeMemberEntry
	// closing counts session DELETEs registered or in flight, which
	// shutdown drains. Guarded by closingMu, not a WaitGroup, so a close
	// registered after shutdown began cannot panic it.
	closingMu sync.Mutex
	closingN  int
	drained   *sync.Cond
}

// codeMemberEntry holds one key's current session.
type codeMemberEntry struct {
	key string
	// slot serializes connecting, not calls: the SDK runs concurrent calls on one session.
	slot chan struct{}

	mu      sync.Mutex
	current *pooledMemberSession
}

// pooledMemberSession is closed once retired and no operation still leases it,
// so retiring it never cuts off a sibling call in flight.
type pooledMemberSession struct {
	session *mcp.ClientSession
	rt      *memberRoundTripper
	created time.Time
	// witness orders listings, which replace the session's listing witness,
	// before calls that a live grant authorizes by that witness.
	witness *semaphore.Weighted

	// Guarded by the entry's mu.
	leases   int
	retired  bool
	listed   bool
	lastUsed time.Time
	idle     *time.Timer
	// idleGen invalidates an idle timer whose callback already fired.
	idleGen uint64
	// verifying is closed when a pending ping of the idle session settles;
	// every borrower waits for it before using the session.
	verifying chan struct{}
}

// maxWitnessReaders bounds the calls that share a session's witness at once.
const maxWitnessReaders = 1 << 20

func newCodeMemberSessions(logger *slog.Logger, idleTTL, maxAge, pingAfter time.Duration, maxEntries int) *codeMemberSessions {
	p := &codeMemberSessions{logger: logger, idleTTL: idleTTL, maxAge: maxAge, pingAfter: pingAfter, maxEntries: maxEntries, mu: sync.Mutex{}, closed: false, entries: map[string]*codeMemberEntry{}, closingMu: sync.Mutex{}, closingN: 0, drained: nil}
	p.drained = sync.NewCond(&p.closingMu)
	return p
}

// codeMemberSessionKey binds a session to the member's destination and to
// the route and credential the call resolved, so a renewed token or an
// edited upstream URL or header gets its own.
func codeMemberSessionKey(member metaMember, dial memberDial) string {
	return codeMemberDestination(member) + "\x00" + dial.sessionKey
}

// pooledLease is acquire's result. A nil lease with a nil error means the
// pool will not hold this session (closed or full); session, if set, then
// belongs to the caller. A failed connect returns its round tripper for
// classification.
type pooledLease struct {
	ctx     context.Context //nolint:containedctx // the refreshed authority to dispatch with
	entry   *codeMemberEntry
	lease   *pooledMemberSession
	session *mcp.ClientSession
	rt      *memberRoundTripper
	// fresh marks a session this call connected; stale one idle past
	// pingAfter, which this caller must ping and then settle.
	fresh bool
	stale bool
	// verifying, when set, is another caller's pending ping to wait for.
	verifying chan struct{}
}

// acquire leases the open session for key, connecting on first use.
func (p *codeMemberSessions) acquire(ctx context.Context, key string, recheck func(context.Context) (context.Context, error), connect func(context.Context) (*mcp.ClientSession, *memberRoundTripper, error)) (pooledLease, error) {
	none := pooledLease{ctx: ctx, entry: nil, lease: nil, session: nil, rt: nil, fresh: false, stale: false, verifying: nil}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return none, nil
	}
	entry, ok := p.entries[key]
	if !ok {
		if len(p.entries) >= p.maxEntries && !p.evictIdleLocked(ctx) {
			p.mu.Unlock()
			return none, nil
		}
		entry = &codeMemberEntry{key: key, slot: make(chan struct{}, 1), mu: sync.Mutex{}, current: nil}
		p.entries[key] = entry
	}
	p.mu.Unlock()

	if lease, stale, verifying := p.lease(ctx, entry); lease != nil {
		return pooledLease{ctx: ctx, entry: entry, lease: lease, session: lease.session, rt: lease.rt, fresh: false, stale: stale, verifying: verifying}, nil
	}
	waited := false
	select {
	case entry.slot <- struct{}{}:
	default:
		waited = true
		select {
		case entry.slot <- struct{}{}:
		case <-ctx.Done():
			return none, fmt.Errorf("wait for member session: %w", ctx.Err())
		}
	}
	defer func() { <-entry.slot }()
	if waited {
		fresh, err := recheck(ctx)
		if err != nil {
			return none, fmt.Errorf("recheck admission after waiting: %w", err)
		}
		ctx = fresh
		none.ctx = ctx
	}
	if lease, stale, verifying := p.lease(ctx, entry); lease != nil {
		return pooledLease{ctx: ctx, entry: entry, lease: lease, session: lease.session, rt: lease.rt, fresh: false, stale: stale, verifying: verifying}, nil
	}
	// The SDK keeps the connect context's values for the connection's
	// lifetime, so none of this call's own state may ride along.
	session, rt, err := connect(withoutCallState{ctx})
	if err != nil {
		p.forget(entry)
		none.rt = rt
		return none, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.entries[key] != entry {
		none.session, none.rt, none.fresh = session, rt, true
		return none, nil
	}
	now := time.Now()
	lease := &pooledMemberSession{session: session, rt: rt, created: now, witness: semaphore.NewWeighted(maxWitnessReaders), leases: 1, retired: false, listed: false, lastUsed: now, idle: nil, idleGen: 0, verifying: nil}
	entry.mu.Lock()
	entry.current = lease
	entry.mu.Unlock()
	return pooledLease{ctx: ctx, entry: entry, lease: lease, session: session, rt: rt, fresh: true, stale: false, verifying: nil}, nil
}

// forget drops an entry left without a session, such as after a failed connect.
func (p *codeMemberSessions) forget(entry *codeMemberEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.current == nil && p.entries[entry.key] == entry {
		delete(p.entries, entry.key)
	}
}

// evictIdleLocked retires one session nobody leases to make room. Requires p.mu.
func (p *codeMemberSessions) evictIdleLocked(ctx context.Context) bool {
	for _, entry := range p.entries {
		entry.mu.Lock()
		lease := entry.current
		if lease == nil || lease.leases == 0 {
			closeNow := lease != nil && p.retireLocked(entry, lease)
			if entry.current == nil {
				delete(p.entries, entry.key)
			}
			entry.mu.Unlock()
			if closeNow {
				p.closeAsync(ctx, lease.session)
			}
			return true
		}
		entry.mu.Unlock()
	}
	return false
}

// lease takes the entry's session unless it is past its maximum age. A
// session idle long enough makes this caller its pinger (stale); while that
// ping is pending, other callers get its channel to wait on.
func (p *codeMemberSessions) lease(ctx context.Context, entry *codeMemberEntry) (*pooledMemberSession, bool, chan struct{}) {
	p.mu.Lock()
	entry.mu.Lock()
	lease := entry.current
	if lease == nil {
		entry.mu.Unlock()
		p.mu.Unlock()
		return nil, false, nil
	}
	if p.maxAge > 0 && time.Since(lease.created) > p.maxAge {
		closeNow := p.retireLocked(entry, lease)
		entry.mu.Unlock()
		p.mu.Unlock()
		if closeNow {
			p.closeAsync(ctx, lease.session)
		}
		return nil, false, nil
	}
	p.mu.Unlock()
	defer entry.mu.Unlock()
	lease.stopIdle()
	lease.leases++
	if lease.verifying != nil {
		return lease, false, lease.verifying
	}
	if p.pingAfter > 0 && lease.leases == 1 && time.Since(lease.lastUsed) > p.pingAfter {
		lease.verifying = make(chan struct{})
		return lease, true, nil
	}
	return lease, false, nil
}

// settle ends a pending ping, retiring the session first if it failed, so
// waiters wake to the outcome.
func (p *codeMemberSessions) settle(ctx context.Context, entry *codeMemberEntry, lease *pooledMemberSession, healthy bool) {
	if !healthy {
		p.retire(ctx, entry, lease)
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if lease.verifying != nil {
		close(lease.verifying)
		lease.verifying = nil
	}
}

func (e *codeMemberEntry) retired(lease *pooledMemberSession) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lease.retired
}

func (l *pooledMemberSession) stopIdle() {
	l.idleGen++
	if l.idle != nil {
		l.idle.Stop()
		l.idle = nil
	}
}

func (e *codeMemberEntry) listed(lease *pooledMemberSession) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lease.listed && !lease.retired
}

func (e *codeMemberEntry) markListed(lease *pooledMemberSession) {
	e.mu.Lock()
	defer e.mu.Unlock()
	lease.listed = true
}

// retireLocked stops handing out lease's session and reports whether it
// should close now. Requires p.mu and entry.mu.
func (p *codeMemberSessions) retireLocked(entry *codeMemberEntry, lease *pooledMemberSession) bool {
	if entry.current == lease {
		entry.current = nil
	}
	if entry.current == nil && p.entries[entry.key] == entry {
		delete(p.entries, entry.key)
	}
	closeNow := !lease.retired && lease.leases == 0
	lease.retired = true
	lease.stopIdle()
	if closeNow {
		p.registerClose()
	}
	return closeNow
}

// retire stops handing out lease's session; the next operation connects again.
func (p *codeMemberSessions) retire(ctx context.Context, entry *codeMemberEntry, lease *pooledMemberSession) {
	p.mu.Lock()
	entry.mu.Lock()
	closeNow := p.retireLocked(entry, lease)
	entry.mu.Unlock()
	p.mu.Unlock()
	if closeNow {
		p.closeAsync(ctx, lease.session)
	}
}

func (p *codeMemberSessions) release(ctx context.Context, entry *codeMemberEntry, lease *pooledMemberSession) {
	entry.mu.Lock()
	lease.leases--
	closeNow := lease.retired && lease.leases == 0
	if closeNow {
		p.registerClose()
	}
	if !lease.retired && lease.leases == 0 {
		lease.lastUsed = time.Now()
		if p.idleTTL > 0 {
			lease.stopIdle()
			gen := lease.idleGen
			detached := context.WithoutCancel(ctx)
			lease.idle = time.AfterFunc(p.idleTTL, func() { p.expire(detached, entry, lease, gen) })
		}
	}
	entry.mu.Unlock()
	if closeNow {
		p.closeAsync(ctx, lease.session)
	}
}

// expire retires a session still idle under the timer generation that fired.
func (p *codeMemberSessions) expire(ctx context.Context, entry *codeMemberEntry, lease *pooledMemberSession, gen uint64) {
	p.mu.Lock()
	entry.mu.Lock()
	closeNow := false
	if lease.idleGen == gen && lease.leases == 0 && !lease.retired {
		closeNow = p.retireLocked(entry, lease)
	}
	entry.mu.Unlock()
	p.mu.Unlock()
	if closeNow {
		p.closeAsync(ctx, lease.session)
	}
}

// close ends the pool. Each idle session closes now, waiting for its DELETE
// when wait is set; a leased one closes when its last operation releases it,
// and a connect still in flight keeps its session for its caller.
func (p *codeMemberSessions) close(ctx context.Context, wait bool) {
	p.mu.Lock()
	p.closed = true
	var idle []*mcp.ClientSession
	for _, entry := range p.entries {
		entry.mu.Lock()
		if lease := entry.current; lease != nil && p.retireLocked(entry, lease) {
			idle = append(idle, lease.session)
		}
		entry.mu.Unlock()
	}
	p.entries = map[string]*codeMemberEntry{}
	p.mu.Unlock()
	for _, session := range idle {
		p.closeAsync(ctx, session)
	}
	if wait {
		p.closingMu.Lock()
		for p.closingN > 0 {
			p.drained.Wait()
		}
		p.closingMu.Unlock()
	}
}

// registerClose counts a DELETE before its session leaves every lock, so
// shutdown never misses it.
func (p *codeMemberSessions) registerClose() {
	p.closingMu.Lock()
	p.closingN++
	p.closingMu.Unlock()
}

// closeAsync runs a DELETE registerClose already counted.
func (p *codeMemberSessions) closeAsync(ctx context.Context, session *mcp.ClientSession) {
	go func() {
		defer func() {
			p.closingMu.Lock()
			p.closingN--
			if p.closingN == 0 {
				p.drained.Broadcast()
			}
			p.closingMu.Unlock()
		}()
		p.closeSession(ctx, session)
	}()
}

// closeSession's DELETE is bounded by the round tripper's own close budget.
func (p *codeMemberSessions) closeSession(ctx context.Context, session *mcp.ClientSession) {
	if err := session.Close(); err != nil {
		p.logger.DebugContext(context.WithoutCancel(ctx), "close code mode member session", attr.SlogError(err))
	}
}

// withoutCallState hides one call's dispatch state from a connection that outlives it.
type withoutCallState struct{ context.Context } //nolint:containedctx // context decorator that filters values

func (c withoutCallState) Value(key any) any {
	switch key.(type) {
	case metaCodeInvocationKey, memberToolResultKey, memberAttemptKey, codeMemberSessionsKey:
		return nil
	}
	return c.Context.Value(key)
}

// memberSession is one operation's member session: its own, or a lease on a
// pooled one with a fresh attempt for this operation.
type memberSession struct {
	session *mcp.ClientSession
	outcome memberOutcome
	slug    string

	scope *codeSessionScope
	entry *codeMemberEntry
	lease *pooledMemberSession
}

// openMemberSession connects to a member, leasing a pooled session for it
// when the operation runs inside Code Mode. A failure is member-scoped.
// The returned context carries the operation's attempt and authority and
// must be used for it.
func (s *Service) openMemberSession(ctx context.Context, logger *slog.Logger, dial memberDial, member metaMember) (context.Context, *memberSession, error) {
	connect := func(ctx context.Context) (*mcp.ClientSession, *memberRoundTripper, error) {
		return s.connectMetaMember(ctx, logger, dial.build, memberSessionCloseTimeout)
	}
	own := func(ctx context.Context) (context.Context, *memberSession, error) {
		session, rt, err := connect(ctx)
		if err != nil {
			return ctx, nil, memberClientFailure(ctx, logger, rt, dial, member, err)
		}
		return ctx, &memberSession{session: session, outcome: rt, slug: member.slug, scope: nil, entry: nil, lease: nil}, nil
	}
	scope, _ := ctx.Value(codeMemberSessionsKey{}).(*codeSessionScope)
	if scope == nil {
		return own(ctx)
	}
	key := scope.prefix + "\x00" + codeMemberSessionKey(member, dial)
	recheck := func(ctx context.Context) (context.Context, error) { return scope.recheck(ctx, member.slug) }
	// A stale session that fails its ping is retired and connected again once.
	for range 2 {
		got, err := scope.pool.acquire(ctx, key, recheck, connect)
		ctx = got.ctx
		switch {
		case err != nil && got.rt == nil:
			return ctx, nil, &metaMemberError{message: fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug)}
		case err != nil:
			return ctx, nil, memberClientFailure(ctx, logger, got.rt, dial, member, err)
		case got.lease == nil && got.session != nil:
			return ctx, &memberSession{session: got.session, outcome: got.rt, slug: member.slug, scope: nil, entry: nil, lease: nil}, nil
		case got.lease == nil:
			// The pool will not hold this session; serve the call on its own.
			return own(ctx)
		}
		attempt := newMemberAttempt(dial.build)
		callCtx := context.WithValue(ctx, memberAttemptKey{}, attempt)
		ms := &memberSession{session: got.session, outcome: attempt, slug: member.slug, scope: scope, entry: got.entry, lease: got.lease}
		switch {
		case got.stale:
			// MCP ping has no side effects, so it is safe to learn here,
			// rather than from a tools/call, that the member expired an idle session.
			healthy := got.session.Ping(callCtx, nil) == nil
			scope.pool.settle(ctx, got.entry, got.lease, healthy)
			if healthy {
				return callCtx, ms, nil
			}
		case got.verifying != nil:
			select {
			case <-got.verifying:
			case <-ctx.Done():
				scope.pool.release(ctx, got.entry, got.lease)
				return ctx, nil, &metaMemberError{message: fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug)}
			}
			healthy := !got.entry.retired(got.lease)
			base := ctx
			if healthy {
				base = callCtx
			}
			fresh, err := scope.recheck(base, member.slug)
			if err != nil {
				scope.pool.release(ctx, got.entry, got.lease)
				return ctx, nil, &metaMemberError{message: fmt.Sprintf("server %q is no longer available to this run", member.slug)}
			}
			if healthy {
				return fresh, ms, nil
			}
			// The ping failed: connect again under the refreshed authority.
			scope.pool.release(ctx, got.entry, got.lease)
			ctx = fresh
			continue
		default:
			return callCtx, ms, nil
		}
		scope.pool.release(ctx, got.entry, got.lease)
	}
	return own(ctx)
}

func (m *memberSession) listed() bool {
	return m.lease != nil && m.entry.listed(m.lease)
}

func (m *memberSession) markListed() {
	if m.lease != nil {
		m.entry.markListed(m.lease)
	}
}

// listing holds the session's witness exclusively while a listing replaces it.
func (m *memberSession) listing(ctx context.Context) (context.Context, func(), error) {
	return m.hold(ctx, maxWitnessReaders)
}

// tryListing takes the witness exclusively only if no call holds it.
func (m *memberSession) tryListing() (func(), bool) {
	if m.lease == nil {
		return func() {}, true
	}
	if !m.lease.witness.TryAcquire(maxWitnessReaders) {
		return nil, false
	}
	return func() { m.lease.witness.Release(maxWitnessReaders) }, true
}

// witnessed holds the session's witness steady for a call a live grant authorizes.
func (m *memberSession) witnessed(ctx context.Context) (context.Context, func(), error) {
	return m.hold(ctx, 1)
}

// hold returns the context to continue with: refreshed if it had to wait.
func (m *memberSession) hold(ctx context.Context, weight int64) (context.Context, func(), error) {
	if m.lease == nil {
		return ctx, func() {}, nil
	}
	release := func() { m.lease.witness.Release(weight) }
	if m.lease.witness.TryAcquire(weight) {
		return ctx, release, nil
	}
	if err := m.lease.witness.Acquire(ctx, weight); err != nil {
		return ctx, nil, fmt.Errorf("wait for member session listing: %w", err)
	}
	fresh, err := m.scope.recheck(ctx, m.slug)
	if err != nil {
		release()
		return ctx, nil, fmt.Errorf("recheck admission after waiting: %w", err)
	}
	return fresh, release, nil
}

// unsent reports a pooled session's call that never reached the member's
// proxy, because a sibling's failure had already broken the session.
func (m *memberSession) unsent() bool {
	attempt, ok := m.outcome.(*memberAttempt)
	if m.lease == nil || !ok {
		return false
	}
	attempt.mu.Lock()
	defer attempt.mu.Unlock()
	return !attempt.sentCall
}

// fail retires a pooled session unless the member itself refused the call
// over a successful exchange, which leaves the session usable. A synthesized
// error or a failed status may mean a broken connection.
func (m *memberSession) fail(ctx context.Context) {
	if m.lease == nil {
		return
	}
	if attempt, ok := m.outcome.(*memberAttempt); ok && attempt.memberRefused() && upstreamStatusOK(attempt.status()) {
		return
	}
	m.scope.pool.retire(ctx, m.entry, m.lease)
}

func (m *memberSession) release(ctx context.Context, logger *slog.Logger) {
	if m.lease != nil {
		m.scope.pool.release(ctx, m.entry, m.lease)
		return
	}
	_ = o11y.LogDefer(ctx, logger, "close member session", m.session.Close)
}
