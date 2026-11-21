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

	// defaultServedInterval is how often one connection's served requests are written. It
	// is the grain a recovery shows at, and matches the sessions' last-used stamps.
	defaultServedInterval = 5 * time.Minute

	// defaultServedFlushInterval is how long a served connection waits in memory.
	defaultServedFlushInterval = 10 * time.Second

	// defaultServedStampBudget bounds the reported-client reads of one flush, so failure rows
	// queued behind it wait no longer. Past it a row's family comes from its User-Agent and catalog.
	defaultServedStampBudget = time.Second

	// servedMaxConnections bounds the memory of the write times. An evicted connection writes early.
	servedMaxConnections = 25_000

	// servedMaxPending bounds what one flush holds. More connections than this wait for the next one.
	servedMaxPending = 10_000

	// dropPendingFull counts connections a full flush turned away, once per flush. They retry on the next.
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

// servedTimes are when a connection was last queued as served and as answered by its server.
type servedTimes struct{ served, answered time.Time }

// servedRow is a connection waiting to be written.
type servedRow struct {
	row        Row
	answeredAt time.Time
}

// servedConnections holds each connection to one write per interval, and what the next flush writes.
type servedConnections struct {
	mu         sync.Mutex
	interval   time.Duration
	maxPending int
	written    *simplelru.LRU[servedKey, servedTimes]
	pending    map[servedKey]servedRow

	// turnedAway holds the connections a full flush turned away, so each counts once per flush.
	turnedAway map[servedKey]struct{}
}

func newServedConnections(interval time.Duration, maxConnections, maxPending int) *servedConnections {
	written, err := simplelru.NewLRU[servedKey, servedTimes](maxConnections, nil)
	if err != nil {
		// Only a non-positive size fails, and callers pass constants.
		panic("served connections size must be positive")
	}
	return &servedConnections{mu: sync.Mutex{}, interval: interval, maxPending: maxPending, written: written, pending: map[servedKey]servedRow{}, turnedAway: map[servedKey]struct{}{}}
}

// note queues the connection if it is due: nothing was queued for it within the interval, or
// its server answered and no answer was. A full flush turns it away until the next one.
func (s *servedConnections) note(key servedKey, row Row, answered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := row.EventTime
	last, _ := s.written.Get(key)
	if now.Sub(last.served) < s.interval && (!answered || now.Sub(last.answered) < s.interval) {
		return
	}
	queued, waiting := s.pending[key]
	if !waiting && len(s.pending) >= s.maxPending {
		if len(s.turnedAway) < s.maxPending {
			s.turnedAway[key] = struct{}{}
		}
		return
	}
	queued.row, last.served = row, now
	if answered {
		queued.answeredAt, last.answered = now, now
	}
	s.pending[key] = queued
	s.written.Add(key, last)
}

// take returns what is waiting and how many connections were turned away, and starts a new flush.
func (s *servedConnections) take() (map[servedKey]servedRow, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending, turnedAway := s.pending, len(s.turnedAway)
	s.pending = map[servedKey]servedRow{}
	clear(s.turnedAway)
	return pending, turnedAway
}

// Served records that a request on a connection was served without an error, at most once
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
	if r.capture != nil {
		if r.captureServed == nil {
			return
		}
		if reason, _ := r.organizationAllowed(row.OrganizationID, r.cfg.logsCheckTimeout); reason == "" {
			r.stampClient(&row)
			r.captureServed(servedOf(servedRow{row: row, answeredAt: answeredAt(row, answered)}))
		}
		return
	}
	if r.served == nil || r.logsOffCached(row.OrganizationID) {
		return
	}
	r.served.note(servedKey{
		organizationID: row.OrganizationID, serverKind: row.ServerKind, serverID: row.ServerID, memberID: row.MemberID,
		subjectURN: row.SubjectURN, appKey: row.AppKey, remoteSessionClientID: row.RemoteSessionClientID, userAgent: row.client.userAgent,
	}, row, answered)
}

func answeredAt(row Row, answered bool) time.Time {
	if answered {
		return row.EventTime
	}
	return time.Time{}
}

func servedOf(s servedRow) Served {
	return Served{
		OrganizationID: s.row.OrganizationID, ProjectID: s.row.ProjectID, ServerKind: s.row.ServerKind, ServerID: s.row.ServerID,
		MemberID: s.row.MemberID, SubjectURN: s.row.SubjectURN, AppKey: s.row.AppKey, ClientFamily: s.row.ClientFamily,
		RemoteSessionClientID: s.row.RemoteSessionClientID, ServedAt: s.row.EventTime, AnsweredAt: s.answeredAt,
	}
}

// logsOffCached reports a fresh logs-off answer already held for an organization, without asking.
func (r *Recorder) logsOffCached(organizationID string) bool {
	if r.referableOrgs == nil {
		return false
	}
	r.referableMu.Lock()
	cached, ok := r.referableOrgs.Get(organizationID)
	r.referableMu.Unlock()
	return ok && !cached.allowed && time.Since(cached.at) < cached.ttl()
}

// rememberLogs keeps a logs answer the writer read, for references and for Served.
func (r *Recorder) rememberLogs(organizationID string, allowed bool) {
	if r.referableOrgs == nil {
		return
	}
	r.referableMu.Lock()
	r.referableOrgs.Add(organizationID, referableOrg{allowed: allowed, at: time.Now()})
	r.referableMu.Unlock()
}

// flushServed writes the connections waiting, once, for organizations with logs on. It runs on the writer.
func (r *Recorder) flushServed() {
	if r.served == nil {
		return
	}
	pending, turnedAway := r.served.take()
	r.servedDrop(context.Background(), dropPendingFull, turnedAway, nil)
	if len(pending) == 0 {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			r.servedDrop(context.Background(), dropPanic, len(pending), fmt.Errorf("connection served writer: %v", recovered))
		}
	}()
	organizations := map[string]dropReason{}
	rows := make([]Served, 0, len(pending))
	read, stampUntil := 0, time.Now().Add(r.cfg.servedStampBudget)
	for _, waiting := range pending {
		if r.writerCtx.Err() != nil {
			break
		}
		read++
		organizationID := waiting.row.OrganizationID
		reason, checked := organizations[organizationID]
		if !checked {
			var err error
			reason, err = r.organizationAllowed(organizationID, r.cfg.logsCheckTimeout)
			organizations[organizationID] = reason
			if err != nil {
				r.servedDrop(context.Background(), reason, 1, err)
				continue
			}
			r.rememberLogs(organizationID, reason == "")
		}
		if reason != "" {
			r.servedDrop(context.Background(), reason, 1, nil)
			continue
		}
		if time.Now().After(stampUntil) {
			waiting.row.client.reported = nil
		}
		r.stampClient(&waiting.row)
		rows = append(rows, servedOf(waiting))
	}
	if r.writerCtx.Err() != nil {
		r.servedDrop(context.Background(), dropShutdown, len(rows)+len(pending)-read, nil)
		return
	}
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
		r.servedDrop(ctx, reason, len(rows), err)
		return
	}
	if r.servedWritten != nil {
		r.servedWritten.Add(context.Background(), int64(len(rows)))
	}
}

// abandonServed counts the connections still waiting as lost to shutdown.
func (r *Recorder) abandonServed() {
	if r.served == nil {
		return
	}
	pending, turnedAway := r.served.take()
	r.servedDrop(context.Background(), dropPendingFull, turnedAway, nil)
	r.servedDrop(context.Background(), dropShutdown, len(pending), nil)
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
