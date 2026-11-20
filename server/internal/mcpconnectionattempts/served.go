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
	// whether a person connected again after a failure needs no finer grain, and a recorded
	// failure reopens the window, so the first request served after it still writes.
	servedInterval = time.Hour

	// servedRetryDelay is how long a connection whose row the limits held back waits before
	// trying again, so a busy organization costs one try a minute per connection, not one a request.
	servedRetryDelay = time.Minute

	// servedMaxConnections bounds memory to about 10 MiB. An evicted connection writes its next row early.
	servedMaxConnections = 25_000

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

// servedKey is one connection: a person on one server, or gateway member, through one app.
type servedKey struct {
	organizationID string
	serverID       uuid.UUID
	memberID       uuid.UUID
	subjectKind    urn.SessionSubjectKind
	subjectID      string
	appKey         string
}

func storedAppKey(appKey string) string {
	if appKey == "" {
		return AppUnverified.key
	}
	return appKey
}

// servedKeyOfRow is the connection a failure row is about. A malformed id names no connection anyone serves.
func servedKeyOfRow(row Row) servedKey {
	kind, id, _ := strings.Cut(row.SubjectURN, ":")
	server, _ := uuid.Parse(row.ServerID)
	member, _ := uuid.Parse(row.MemberID)
	return servedKey{organizationID: row.OrganizationID, serverID: server, memberID: member, subjectKind: urn.SessionSubjectKind(kind), subjectID: id, appKey: row.AppKey}
}

// servedConnections samples served rows: one per connection per servedInterval. Unlike Once rows,
// a sampled-out row is not counted, since a success needs no total. It has its own lock, so a
// served request never waits on Once rows.
type servedConnections struct {
	mu   sync.Mutex
	last *simplelru.LRU[servedKey, time.Time]
	orgs *simplelru.LRU[string, *rate.Limiter]
}

func newServedConnections() *servedConnections {
	last, lastErr := simplelru.NewLRU[servedKey, time.Time](servedMaxConnections, nil)
	orgs, orgsErr := simplelru.NewLRU[string, *rate.Limiter](onceMaxOrgs, nil)
	if lastErr != nil || orgsErr != nil {
		// Only a non-positive size fails, and these are constants.
		panic("served connections sizes must be positive")
	}
	return &servedConnections{mu: sync.Mutex{}, last: last, orgs: orgs}
}

// claim reports whether a connection's served row is due, and if so claims the window, so
// concurrent requests write one row.
func (s *servedConnections) claim(key servedKey, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.last.Get(key); ok && now.Sub(last) < servedInterval {
		return false
	}
	s.last.Add(key, now)
	return true
}

// release gives back a claim whose row was not queued, so the next request tries again.
func (s *servedConnections) release(key servedKey, claimedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.last.Peek(key); ok && last.Equal(claimedAt) {
		s.last.Remove(key)
	}
}

// retryLater keeps a claim the limits held back for servedRetryDelay only.
func (s *servedConnections) retryLater(key servedKey, claimedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.last.Peek(key); ok && last.Equal(claimedAt) {
		s.last.Add(key, claimedAt.Add(servedRetryDelay-servedInterval))
	}
}

// forget reopens a connection's window after a recorded failure.
func (s *servedConnections) forget(key servedKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.last.Remove(key)
}

func (s *servedConnections) allowOrganization(organizationID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	org, ok := s.orgs.Get(organizationID)
	if !ok {
		org = rate.NewLimiter(servedOrgPerSecond, servedOrgBurst)
		s.orgs.Add(organizationID, org)
	}
	return org.AllowN(now, 1)
}

// servedQueueReserved reports that the rest of the queue is kept for failures and real attempts.
func (r *Recorder) servedQueueReserved() bool {
	return len(r.queue)*servedQueueShareDenominator >= cap(r.queue)*servedQueueShareNumerator
}

// Served records that a request was served for a named caller, as ResultServed at the request
// step: at most one row per person, server or gateway member, and app per servedInterval in this
// process. It writes nothing for a missing or anonymous subject, for a ctx whose failure mark is
// set, or for a caller that went away. When no row is due it takes one lock and returns, without
// calling details, which supplies the row's other options, such as its app version.
func (r *Recorder) Served(ctx context.Context, scope Scope, member uuid.UUID, subject *urn.SessionSubject, details func() []Option) (accepted bool) {
	if r == nil || r.served == nil || subject == nil || subject.IsZero() || subject.Kind == urn.SessionSubjectKindAnonymous ||
		!scope.valid() || ctx.Err() != nil || Failed(ctx) {
		return false
	}
	key := servedKey{organizationID: scope.organizationID, serverID: scope.serverID, memberID: member, subjectKind: subject.Kind, subjectID: subject.ID, appKey: storedAppKey(scope.appKey)}
	now := time.Now()
	if !r.served.claim(key, now) {
		return false
	}
	deferred := false
	defer func() {
		// Recording must never panic a request.
		if recovered := recover(); recovered != nil {
			r.drop(ctx, dropPanic, 1, fmt.Errorf("connection events served: %v", recovered))
			accepted = false
		}
		if !accepted && !deferred {
			r.served.release(key, now)
		}
	}()
	if r.limiter != nil {
		var reason dropReason
		switch {
		case r.servedQueueReserved():
			reason = dropQueueReserve
		case !r.served.allowOrganization(scope.organizationID, now):
			reason = dropRateLimited
		}
		if reason != "" {
			r.served.retryLater(key, now)
			deferred = true
			r.drop(ctx, reason, 1, nil)
			return false
		}
	}
	var opts []Option
	if details != nil {
		opts = details()
	}
	opts = append(opts, Subject(subject), Member(member))
	return r.record(ctx, Begin(scope), StepMCPRequest, ResultServed, true, limitNone, opts)
}

// forgetServed reopens the served window of the connection a queued request failure is about.
func (r *Recorder) forgetServed(step Step, what Happened, row Row) {
	if _, failed := what.(Code); !failed || step != StepMCPRequest || r.served == nil || row.SubjectURN == "" {
		return
	}
	r.served.forget(servedKeyOfRow(row))
}
