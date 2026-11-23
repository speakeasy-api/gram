package mcpconnectionattempts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	lastServedTable = "mcp_connection_last_served"

	lastServedInsert = `INSERT INTO %s (
		organization_id, project_id, server_kind, server_id, member_id, subject_urn,
		app_key, client_family, remote_session_client_id, last_served_at, last_answered_at)`

	meterServedWritten = "mcp.connection.served.written"
	meterServedDropped = "mcp.connection.served.dropped"

	// defaultServedInterval is how often one connection's served requests are written. Stored
	// times trail the latest request by at most this, and it matches the sessions' last-used stamps.
	defaultServedInterval = 5 * time.Minute

	// defaultServedFlushInterval is how long a served connection waits in memory.
	defaultServedFlushInterval = 10 * time.Second

	// defaultServedFlushBudget bounds the logs checks and reported-client reads of one flush, so
	// failure rows queued behind it wait no longer. Rows it does not reach wait for the next flush.
	defaultServedFlushBudget = time.Second

	// servedMaxConnections bounds the connections held. An evicted one's unwritten times go to the next flush.
	servedMaxConnections = 25_000

	// servedMaxPending bounds what one flush holds. More connections than this wait for the next one.
	servedMaxPending = 10_000

	// dropPendingFull counts evicted connections whose unwritten times a full flush could not take.
	dropPendingFull dropReason = "pending_full"
)

// Served is one connection's latest served request, as stored.
type Served struct {
	OrganizationID        string
	ProjectID             uuid.UUID
	ServerKind            string
	ServerID              string
	MemberID              string
	SubjectURN            string
	AppKey                string
	ClientFamily          string
	RemoteSessionClientID string

	// ServedAt is when the request was served.
	ServedAt time.Time

	// AnsweredAt is when the server itself last answered, zero when Speakeasy answered.
	AnsweredAt time.Time
}

// servedKey is one connection as the request names it. The User-Agent stands in for the
// client family, which is only worked out on the writer.
type servedKey struct {
	organizationID        string
	serverKind            string
	serverID              string
	memberID              string
	subjectURN            string
	appKey                string
	remoteSessionClientID string
	userAgent             string
}

// servedTimes are a connection's served and answered request times.
type servedTimes struct{ served, answered time.Time }

// servedRequest is what a served row needs beyond its key.
type servedRequest struct {
	projectID uuid.UUID
	at        time.Time

	// answeredAt is the latest time the server itself answered, zero when only Speakeasy did.
	answeredAt time.Time
	client     requestClient
}

// merged keeps the later request and the later answer.
func (w servedRequest) merged(o servedRequest) servedRequest {
	if o.at.After(w.at) {
		w.projectID, w.at, w.client = o.projectID, o.at, o.client
	}
	if o.answeredAt.After(w.answeredAt) {
		w.answeredAt = o.answeredAt
	}
	return w
}

// servedState is one connection's last write and the latest request seen since.
type servedState struct {
	// queuedAt is when it was last queued: the request's time, or the flush's for a trailing write.
	queuedAt time.Time
	queued   servedTimes
	latest   servedRequest
}

// unwritten reports a request or answer later than what was last queued.
func (st *servedState) unwritten() bool {
	return st.latest.at.After(st.queued.served) || st.latest.answeredAt.After(st.queued.answered)
}

// servedConnections holds each connection to about one write per interval and what the next flush writes.
// The first request when due is queued at once; later ones are written by a flush once the interval passed.
type servedConnections struct {
	mu         sync.Mutex
	interval   time.Duration
	maxPending int
	states     *simplelru.LRU[servedKey, *servedState]
	pending    map[servedKey]servedRequest

	// unwritten holds the connections with times later than their last write.
	unwritten map[servedKey]*servedState

	// lost counts evicted connections with unwritten times a full flush could not take.
	lost int

	// forgetting is set while forget removes connections, so their removal is not an eviction.
	forgetting bool
}

func newServedConnections(interval time.Duration, maxConnections, maxPending int) *servedConnections {
	s := &servedConnections{mu: sync.Mutex{}, interval: interval, maxPending: maxPending, states: nil, pending: map[servedKey]servedRequest{}, unwritten: map[servedKey]*servedState{}, lost: 0, forgetting: false}
	states, err := simplelru.NewLRU(maxConnections, s.evicted)
	if err != nil {
		// Only a non-positive size fails, and callers pass constants.
		panic("served connections size must be positive")
	}
	s.states = states
	return s
}

// note keeps the request as the connection's latest and queues it if due: nothing was queued
// within the interval, or its server answered and no answer was. Otherwise a later flush writes it.
func (s *servedConnections) note(key servedKey, req servedRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.states.Get(key)
	if !ok {
		st = new(servedState)
		s.states.Add(key, st)
	}
	st.latest = st.latest.merged(req)
	now := req.at
	answered := !req.answeredAt.IsZero()
	if (now.Sub(st.queuedAt) >= s.interval || answered && now.Sub(st.queued.answered) >= s.interval) && s.queue(key, st, now) {
		return
	}
	if st.unwritten() {
		s.unwritten[key] = st
	}
}

// queue moves a connection's latest times to the next flush, unless that flush is full.
func (s *servedConnections) queue(key servedKey, st *servedState, at time.Time) bool {
	waiting, ok := s.pending[key]
	if !ok && len(s.pending) >= s.maxPending {
		return false
	}
	s.pending[key] = waiting.merged(st.latest)
	st.queuedAt, st.queued = at, servedTimes{served: st.latest.at, answered: st.latest.answeredAt}
	delete(s.unwritten, key)
	return true
}

// evicted hands a connection's unwritten times to the next flush, so eviction loses none while it has room.
func (s *servedConnections) evicted(key servedKey, st *servedState) {
	delete(s.unwritten, key)
	if s.forgetting || !st.unwritten() {
		return
	}
	if waiting, ok := s.pending[key]; ok || len(s.pending) < s.maxPending {
		s.pending[key] = waiting.merged(st.latest)
		return
	}
	s.lost++
}

// putBack returns connections a flush did not store to the next one. Past the bound they wait as
// unwritten, so a later flush writes them.
func (s *servedConnections) putBack(rows map[servedKey]servedRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, req := range rows {
		if waiting, ok := s.pending[key]; ok || len(s.pending) < s.maxPending {
			s.pending[key] = waiting.merged(req)
			continue
		}
		st, ok := s.states.Get(key)
		if !ok {
			st = new(servedState)
			s.states.Add(key, st)
		}
		st.latest = st.latest.merged(req)
		st.queuedAt, st.queued = time.Time{}, servedTimes{served: time.Time{}, answered: time.Time{}}
		s.unwritten[key] = st
	}
}

// forget drops what is held for connections a flush did not write because of their organization's
// logs, so their next request is due as soon as logs are on.
func (s *servedConnections) forget(keys []servedKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.forgetting = true
	defer func() { s.forgetting = false }()
	for _, key := range keys {
		s.states.Remove(key)
	}
}

// take returns what the flush at now writes and how many evicted connections were lost. It adds
// each connection with unwritten times last queued at least an interval ago, or every one when final.
func (s *servedConnections) take(now time.Time, final bool) (map[servedKey]servedRequest, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, st := range s.unwritten {
		if !final && now.Sub(st.queuedAt) < s.interval {
			continue
		}
		if !s.queue(key, st, now) {
			break
		}
	}
	pending, lost := s.pending, s.lost
	s.pending, s.lost = map[servedKey]servedRequest{}, 0
	return pending, lost
}

// abandon clears what is left at shutdown and returns how many connections had unwritten times and how many were lost.
func (s *servedConnections) abandon() (left, lost int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	left, lost = len(s.pending), s.lost
	for key := range s.unwritten {
		if _, ok := s.pending[key]; !ok {
			left++
		}
	}
	s.pending, s.lost = map[servedKey]servedRequest{}, 0
	clear(s.unwritten)
	return left, lost
}

// Served records that a request on a connection was served without an error, written about once
// per connection per interval. answered says the server itself answered, not Speakeasy
// from what it holds. opts are the ones the connection's failure rows carry: Subject,
// Member, FromRequest, ReportedClient, and RemoteSessionClient when the person's own
// sign-in to the service was used. A request naming no person, or no server, writes nothing.
func (r *Recorder) Served(ctx context.Context, scope Scope, answered bool, opts ...Option) {
	if r == nil || !scope.valid() || scope.serverID == uuid.Nil || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	// Recording must never panic a request.
	defer func() {
		if recovered := recover(); recovered != nil {
			r.servedDrop(ctx, dropPanic, 1, fmt.Errorf("connection served: %v", recovered))
		}
	}()
	row := newRow(scope, time.Now())
	row.apply(opts)
	if row.SubjectURN == "" || strings.HasPrefix(row.SubjectURN, string(urn.SessionSubjectKindAnonymous)+":") {
		return
	}
	// The table has no device key.
	row.client.ipAddress = ""
	key := servedKey{
		organizationID: row.OrganizationID, serverKind: row.ServerKind, serverID: row.ServerID, memberID: row.MemberID,
		subjectURN: row.SubjectURN, appKey: row.AppKey, remoteSessionClientID: row.RemoteSessionClientID, userAgent: row.client.userAgent,
	}
	req := servedRequest{projectID: row.ProjectID, at: row.EventTime, answeredAt: time.Time{}, client: row.client}
	if answered {
		req.answeredAt = row.EventTime
	}
	if r.capture != nil {
		if r.captureServed == nil {
			return
		}
		if reason, _ := r.organizationAllowed(row.OrganizationID, r.cfg.logsCheckTimeout); reason == "" {
			r.captureServed(r.servedOf(key, req))
		}
		return
	}
	if r.served == nil {
		return
	}
	if reason, cached := r.cachedLogs(row.OrganizationID); cached && reason != "" {
		return
	}
	if stopped := r.admitServed(key, req); stopped {
		r.servedDrop(ctx, dropShutdown, 1, nil)
	}
}

// admitServed notes a served connection under stopMu, so none arrives after the final flush.
func (r *Recorder) admitServed(key servedKey, req servedRequest) (stopped bool) {
	r.stopMu.RLock()
	defer r.stopMu.RUnlock()

	if r.stopped {
		return true
	}
	r.served.note(key, req)
	return false
}

// servedOf names a served connection's client on the writer and returns it as stored.
func (r *Recorder) servedOf(key servedKey, req servedRequest) Served {
	var row Row
	row.OrganizationID, row.client = key.organizationID, req.client
	r.stampClient(&row)
	return Served{
		OrganizationID: key.organizationID, ProjectID: req.projectID, ServerKind: key.serverKind, ServerID: key.serverID,
		MemberID: key.memberID, SubjectURN: key.subjectURN, AppKey: key.appKey, ClientFamily: row.ClientFamily,
		RemoteSessionClientID: key.remoteSessionClientID, ServedAt: req.at, AnsweredAt: req.answeredAt,
	}
}

// cachedLogs returns a fresh logs answer already held for an organization, without asking. A check
// that failed is no answer.
func (r *Recorder) cachedLogs(organizationID string) (reason dropReason, cached bool) {
	if r.referableOrgs == nil {
		return "", false
	}
	r.referableMu.Lock()
	held, ok := r.referableOrgs.Get(organizationID)
	r.referableMu.Unlock()
	switch {
	case !ok || held.failed || time.Since(held.at) >= held.ttl():
		return "", false
	case held.allowed:
		return "", true
	default:
		return dropLogsOff, true
	}
}

// rememberLogs keeps a logs answer the writer read, for references and for Served.
func (r *Recorder) rememberLogs(organizationID string, allowed bool) {
	if r.referableOrgs == nil {
		return
	}
	r.referableMu.Lock()
	r.referableOrgs.Add(organizationID, referableOrg{allowed: allowed, failed: false, at: time.Now()})
	r.referableMu.Unlock()
}

// flushServed writes the connections due at now, once, for organizations with logs on. final takes
// every connection with unwritten times. It runs on the writer, so one flush's logs checks and
// reported-client reads share a budget; connections it does not reach wait for the next flush.
func (r *Recorder) flushServed(now time.Time, final bool) {
	if r.served == nil {
		return
	}
	pending, lost := r.served.take(now, final)
	r.servedDrop(context.Background(), dropPendingFull, lost, nil)
	if len(pending) == 0 {
		return
	}
	var skipped []servedKey
	// Logs turned back on must not find the connection still held back.
	defer func() { r.served.forget(skipped) }()
	taken, putBack := len(pending), 0
	defer func() {
		if recovered := recover(); recovered != nil {
			r.servedDrop(context.Background(), dropPanic, taken-putBack, fmt.Errorf("connection served writer: %v", recovered))
		}
	}()
	organizations := map[string]dropReason{}
	rows := make([]Served, 0, len(pending))
	written := make(map[servedKey]servedRequest, len(pending))
	until := time.Now().Add(r.cfg.servedFlushBudget)
	for key, waiting := range pending {
		if r.writerCtx.Err() != nil || time.Now().After(until) {
			break
		}
		// What is left in pending is what this flush did not reach.
		delete(pending, key)
		organizationID := key.organizationID
		reason, checked := organizations[organizationID]
		if !checked {
			var cached bool
			if reason, cached = r.cachedLogs(organizationID); !cached {
				var err error
				reason, err = r.organizationAllowed(organizationID, min(r.cfg.logsCheckTimeout, time.Until(until)))
				if err != nil && time.Now().After(until) {
					// Cut short by the budget, not failed: it waits for the next flush.
					pending[key] = waiting
					break
				}
				if err != nil {
					organizations[organizationID] = reason
					skipped = append(skipped, key)
					r.servedDrop(context.Background(), reason, 1, err)
					continue
				}
				r.rememberLogs(organizationID, reason == "")
			}
			organizations[organizationID] = reason
		}
		if reason != "" {
			skipped = append(skipped, key)
			r.servedDrop(context.Background(), reason, 1, nil)
			continue
		}
		rows = append(rows, r.servedOf(key, waiting))
		written[key] = waiting
	}
	if r.writerCtx.Err() != nil {
		r.servedDrop(context.Background(), dropShutdown, len(rows)+len(pending), nil)
		return
	}
	putBack = len(pending)
	r.served.putBack(pending)
	if len(rows) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(r.writerCtx, r.cfg.insertTimeout)
	defer cancel()
	// Waits for the commit, so a failure is counted here.
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1, "wait_for_async_insert": 1}))
	if err := appendServedInto(ctx, r.conn, r.cfg.servedTable, rows); err != nil {
		reason := dropInsertFailed
		if exception, ok := errors.AsType[*clickhouse.Exception](err); ok && exception.Code == unknownTableCode {
			reason = dropTableMissing
		}
		// The next flush retries them, unless this was the last.
		if !final {
			r.served.putBack(written)
		}
		r.servedDrop(ctx, reason, len(rows), err)
		return
	}
	if r.servedWritten != nil {
		r.servedWritten.Add(context.Background(), int64(len(rows)))
	}
}

// abandonServed counts the connections still unwritten as lost to shutdown.
func (r *Recorder) abandonServed() {
	if r.served == nil {
		return
	}
	left, lost := r.served.abandon()
	r.servedDrop(context.Background(), dropPendingFull, lost, nil)
	r.servedDrop(context.Background(), dropShutdown, left, nil)
}

// servedDrop counts served connections not written. They are not connection events, so
// they stay out of the events' drop totals and shutdown losses.
func (r *Recorder) servedDrop(ctx context.Context, reason dropReason, n int, cause error) {
	if n <= 0 {
		return
	}
	if r.servedDropped != nil {
		r.servedDropped.Add(ctx, int64(n), metric.WithAttributes(attr.ConnectionDropReason(string(reason))))
	}
	// Expected for any organization with logs off, so it is only counted.
	last := r.lastServedDrop[reason]
	if reason == dropLogsOff || last == nil {
		return
	}
	now := time.Now().UnixNano()
	previous := last.Load()
	if now-previous < int64(dropLogInterval) || !last.CompareAndSwap(previous, now) {
		return
	}
	attrs := []any{attr.SlogConnectionDropReason(string(reason))}
	if cause != nil {
		attrs = append(attrs, attr.SlogError(cause))
	}
	r.logger.WarnContext(ctx, "dropped served connections", attrs...)
}

// appendServedInto inserts served connections into table. The table keeps the latest times per connection.
func appendServedInto(ctx context.Context, conn driver.Conn, table string, rows []Served) error {
	batch, err := conn.PrepareBatch(ctx, fmt.Sprintf(lastServedInsert, table))
	if err != nil {
		return fmt.Errorf("prepare served connections: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return batch.Abort() })

	for _, s := range rows {
		// The epoch is the column's "never", below every real time.
		answered := time.Unix(0, 0).UTC()
		if !s.AnsweredAt.IsZero() {
			answered = s.AnsweredAt
		}
		if err := batch.Append(s.OrganizationID, s.ProjectID, s.ServerKind, s.ServerID, s.MemberID, s.SubjectURN,
			s.AppKey, s.ClientFamily, s.RemoteSessionClientID, s.ServedAt, answered); err != nil {
			return fmt.Errorf("append served connection: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert served connections: %w", err)
	}
	return nil
}
