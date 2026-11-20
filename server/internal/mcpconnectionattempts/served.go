package mcpconnectionattempts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// servedInterval is how often one connection's served requests write a row. A reader asking
	// whether a person connected again after a failure needs no finer grain, and a failure on the
	// connection's server makes it due again sooner.
	servedInterval = time.Hour

	// servedRetryDelay is the least time between two rows of one connection when a failure made it
	// due early, and how long a connection whose row the limits held back waits before trying
	// again, so a flapping or busy connection costs one row a minute, not one a request.
	servedRetryDelay = time.Minute

	// servedMaxConnections bounds the connections one process tracks. An entry measures about
	// 360 bytes with its LRU element, map slot and unshared key strings, so this is about 34 MiB.
	// An evicted connection writes its next row early, within the organization limit.
	servedMaxConnections = 100_000

	// servedMaxFailures bounds the remembered failures, at about 360 bytes each: about 17 MiB.
	// An evicted failure only means the connection waits out its hour.
	servedMaxFailures = 50_000

	// servedOrgBurst and servedOrgPerSecond cap one organization's served rows per replica, apart
	// from its failure rows, so success rows never spend the budget failures need. 5 a second is
	// 18,000 connections an hour; the burst absorbs a restart, when every connection is due at once.
	servedOrgBurst     = 500
	servedOrgPerSecond = 5

	// servedQueueShareNumerator and servedQueueShareDenominator give served rows only the first
	// half of the queue, below the share failure rows get, so a backlog drops successes first.
	servedQueueShareNumerator   = 1
	servedQueueShareDenominator = 2
)

// servedKey is one connection: a caller on one server, or gateway member, through one app.
type servedKey struct {
	organizationID string
	serverID       uuid.UUID
	memberID       uuid.UUID
	subjectKind    urn.SessionSubjectKind
	subjectID      string
	appKey         string
}

// failureKey is a caller on one server, across apps: one gateway member's failures, or with no
// member, failures of the caller's connection to the server itself, which concern every member.
type failureKey struct {
	organizationID string
	serverID       uuid.UUID
	memberID       uuid.UUID
	subjectKind    urn.SessionSubjectKind
	subjectID      string
}

func (k servedKey) failureKey(member uuid.UUID) failureKey {
	return failureKey{organizationID: k.organizationID, serverID: k.serverID, memberID: member, subjectKind: k.subjectKind, subjectID: k.subjectID}
}

func storedAppKey(appKey string) string {
	if appKey == "" {
		return AppUnverified.key
	}
	return appKey
}

// servedEntry is a connection's last served row, or a row the limits held back.
type servedEntry struct {
	at       time.Time
	deferred bool
}

// claimResult is what a served request's claim decided.
type claimResult int

const (
	claimNotDue claimResult = iota
	claimDeferred
	claimed
)

// servedConnections samples served rows: one per connection per servedInterval, or sooner after a
// failure. Unlike Once rows, a sampled-out row is not counted, since a success needs no total. It
// has its own lock, so a served request never waits on Once rows.
type servedConnections struct {
	mu       sync.Mutex
	last     *simplelru.LRU[servedKey, servedEntry]
	failures *simplelru.LRU[failureKey, time.Time]
	orgs     *simplelru.LRU[string, *rate.Limiter]
}

func newServedConnections() *servedConnections {
	last, lastErr := simplelru.NewLRU[servedKey, servedEntry](servedMaxConnections, nil)
	failures, failuresErr := simplelru.NewLRU[failureKey, time.Time](servedMaxFailures, nil)
	orgs, orgsErr := simplelru.NewLRU[string, *rate.Limiter](onceMaxOrgs, nil)
	if lastErr != nil || failuresErr != nil || orgsErr != nil {
		// Only a non-positive size fails, and these are constants.
		panic("served connections sizes must be positive")
	}
	return &servedConnections{mu: sync.Mutex{}, last: last, failures: failures, orgs: orgs}
}

func (s *servedConnections) dueLocked(key servedKey, now time.Time) bool {
	entry, ok := s.last.Get(key)
	switch {
	case !ok:
		return true
	case entry.deferred:
		return now.Sub(entry.at) >= servedRetryDelay
	case now.Sub(entry.at) >= servedInterval:
		return true
	}
	if now.Sub(entry.at) < servedRetryDelay {
		return false
	}
	if failed, ok := s.failures.Peek(key.failureKey(uuid.Nil)); ok && failed.After(entry.at) {
		return true
	}
	if key.memberID == uuid.Nil {
		return false
	}
	failed, ok := s.failures.Peek(key.failureKey(key.memberID))
	return ok && failed.After(entry.at)
}

// claim decides a served request under one lock. A due connection is claimed, so concurrent
// requests write one row, unless held back: by held, or by the organization limit when limit is
// set. A held-back connection waits servedRetryDelay, even if it was evicted.
func (s *servedConnections) claim(key servedKey, now time.Time, limit, held bool) claimResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.dueLocked(key, now) {
		return claimNotDue
	}
	if held || (limit && !s.orgLocked(key.organizationID).AllowN(now, 1)) {
		s.last.Add(key, servedEntry{at: now, deferred: true})
		return claimDeferred
	}
	s.last.Add(key, servedEntry{at: now, deferred: false})
	return claimed
}

// release gives back a claim whose row was not queued, so the next request tries again.
func (s *servedConnections) release(key servedKey, claimedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.last.Peek(key); ok && !entry.deferred && entry.at.Equal(claimedAt) {
		s.last.Remove(key)
	}
}

// failed remembers a failure, which makes the connections it concerns due again. The stored time
// never moves back, so a slower concurrent failure cannot hide a newer one.
func (s *servedConnections) failed(key failureKey, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.failures.Peek(key); ok && last.After(now) {
		s.failures.Get(key)
		return
	}
	s.failures.Add(key, now)
}

func (s *servedConnections) orgLocked(organizationID string) *rate.Limiter {
	org, ok := s.orgs.Get(organizationID)
	if !ok {
		org = rate.NewLimiter(servedOrgPerSecond, servedOrgBurst)
		s.orgs.Add(organizationID, org)
	}
	return org
}

func (row Row) isServed() bool {
	return row.Step == StepMCPRequest.name && row.StepResult == ResultServed.name
}

// servedQueueReserved reports that the rest of the queue is kept for failures and real attempts.
func (r *Recorder) servedQueueReserved() bool {
	return len(r.queue)*servedQueueShareDenominator >= cap(r.queue)*servedQueueShareNumerator
}

// Served records that a request was served for a named caller, as ResultServed at the request
// step: at most one row per caller, server or gateway member, and app per servedInterval in this
// process, and again after a failure that re-arms it, at most once per servedRetryDelay. It writes
// nothing for a missing or anonymous subject, for a ctx whose failure mark is set, or for a caller
// that went away. When no row is due it takes one lock and returns, without calling row. row
// says whether the caller got a successful result, which may take reading the response, and
// supplies the row's other options, such as its app version; a request that did not succeed
// releases its claim, so the next success writes.
func (r *Recorder) Served(ctx context.Context, scope Scope, member uuid.UUID, subject *urn.SessionSubject, row func() ([]Option, bool)) (accepted bool) {
	if r == nil || r.served == nil || subject == nil || subject.IsZero() || subject.Kind == urn.SessionSubjectKindAnonymous ||
		!scope.valid() || ctx.Err() != nil || Failed(ctx) {
		return false
	}
	key := servedKey{organizationID: scope.organizationID, serverID: scope.serverID, memberID: member, subjectKind: subject.Kind, subjectID: subject.ID, appKey: storedAppKey(scope.appKey)}
	now := time.Now()
	limited := r.limiter != nil
	if r.served.claim(key, now, limited, limited && r.servedQueueReserved()) != claimed {
		return false
	}
	defer func() {
		// Recording must never panic a request.
		if recovered := recover(); recovered != nil {
			r.drop(ctx, dropPanic, 1, fmt.Errorf("connection events served: %v", recovered))
			accepted = false
		}
		if !accepted {
			r.served.release(key, now)
		}
	}()
	opts, succeeded := row()
	if !succeeded {
		return false
	}
	opts = append(opts, Subject(subject), Member(member))
	return r.record(ctx, Begin(scope), StepMCPRequest, ResultServed, true, limitNone, opts)
}

// observeFailure makes a named caller's connections due again before any limit decides whether the
// failure row is written, whatever the step or app: on the row's gateway member only, or with no
// member, on every connection to the row's server.
func (r *Recorder) observeFailure(row Row, now time.Time) {
	if r.served == nil || row.SubjectURN == "" || row.ServerID == "" {
		return
	}
	kind, id, ok := strings.Cut(row.SubjectURN, ":")
	if !ok || urn.SessionSubjectKind(kind) == urn.SessionSubjectKindAnonymous {
		return
	}
	server, err := uuid.Parse(row.ServerID)
	if err != nil {
		return
	}
	member := uuid.Nil
	if row.MemberID != "" {
		if member, err = uuid.Parse(row.MemberID); err != nil {
			return
		}
	}
	r.served.failed(failureKey{organizationID: row.OrganizationID, serverID: server, memberID: member, subjectKind: urn.SessionSubjectKind(kind), subjectID: id}, now)
}
