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

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	lastServedTable = "mcp_connection_last_served"

	lastServedInsert = `INSERT INTO ` + lastServedTable + ` (
		organization_id, project_id, server_kind, server_id, member_id, subject_urn,
		app_key, client_family, remote_session_client_id, last_served_at, last_answered_at)`

	// defaultServedInterval is how often one connection's served requests are written. It
	// is the grain a recovery shows at, and matches the sessions' last-used stamps.
	defaultServedInterval = 5 * time.Minute

	// defaultServedFlushInterval is how long a served connection waits in memory.
	defaultServedFlushInterval = 10 * time.Second

	// servedMaxConnections bounds the memory of the write times. An evicted connection writes early.
	servedMaxConnections = 25_000

	// servedMaxPending bounds what one flush holds. More connections than this wait for the next one.
	servedMaxPending = 10_000
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
	mu       sync.Mutex
	interval time.Duration
	written  *simplelru.LRU[servedKey, servedTimes]
	pending  map[servedKey]servedRow
}

func newServedConnections(interval time.Duration) *servedConnections {
	written, err := simplelru.NewLRU[servedKey, servedTimes](servedMaxConnections, nil)
	if err != nil {
		// Only a non-positive size fails, and it is a constant.
		panic("served connections size must be positive")
	}
	return &servedConnections{mu: sync.Mutex{}, interval: interval, written: written, pending: map[servedKey]servedRow{}}
}

// note queues the connection if it is due: nothing was queued for it within the interval, or
// its server answered and no answer was. It reports false only when the flush is full.
func (s *servedConnections) note(key servedKey, row Row, answered bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := row.EventTime
	last, _ := s.written.Get(key)
	if now.Sub(last.served) < s.interval && (!answered || now.Sub(last.answered) < s.interval) {
		return true
	}
	queued, waiting := s.pending[key]
	if !waiting && len(s.pending) >= servedMaxPending {
		return false
	}
	queued.row, last.served = row, now
	if answered {
		queued.answeredAt, last.answered = now, now
	}
	s.pending[key] = queued
	s.written.Add(key, last)
	return true
}

// take returns what is waiting and starts a new flush.
func (s *servedConnections) take() map[servedKey]servedRow {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending := s.pending
	s.pending = map[servedKey]servedRow{}
	return pending
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
			r.drop(ctx, dropPanic, 1, fmt.Errorf("connection served: %v", recovered))
		}
	}()
	row, err := RowAt(Begin(scope), StepMCPRequest, ResultAllowed, true, time.Now(), opts...)
	if err != nil || row.SubjectURN == "" || strings.HasPrefix(row.SubjectURN, string(urn.SessionSubjectKindAnonymous)+":") {
		return
	}
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
	if r.served == nil {
		return
	}
	key := servedKey{
		organizationID: row.OrganizationID, serverKind: row.ServerKind, serverID: row.ServerID, memberID: row.MemberID,
		subjectURN: row.SubjectURN, appKey: row.AppKey, remoteSessionClientID: row.RemoteSessionClientID, userAgent: row.client.userAgent,
	}
	if !r.served.note(key, row, answered) {
		r.drop(ctx, dropQueueFull, 1, nil)
	}
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

// CaptureServed hands a capturing recorder's served connections to capture. Use
// mcpconnectionattemptstest rather than calling this.
func (r *Recorder) CaptureServed(capture func(Served)) {
	r.captureServed = capture
}

// flushServed writes the connections waiting, once, for organizations with logs on. It runs on the writer.
func (r *Recorder) flushServed() {
	if r.served == nil {
		return
	}
	pending := r.served.take()
	if len(pending) == 0 {
		return
	}
	r.surviving(len(pending), func() {
		organizations := map[string]dropReason{}
		rows := make([]Served, 0, len(pending))
		for _, waiting := range pending {
			reason, checked := organizations[waiting.row.OrganizationID]
			if !checked {
				var err error
				reason, err = r.organizationAllowed(waiting.row.OrganizationID, r.cfg.logsCheckTimeout)
				organizations[waiting.row.OrganizationID] = reason
				if err != nil {
					r.drop(context.Background(), reason, 1, err)
					continue
				}
			}
			if reason != "" {
				r.drop(context.Background(), reason, 1, nil)
				continue
			}
			r.stampClient(&waiting.row)
			rows = append(rows, servedOf(waiting))
		}
		if len(rows) == 0 || r.writerCtx.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.writerCtx, r.cfg.insertTimeout)
		defer cancel()
		// Waits for the commit, so a failure is counted here.
		ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1, "wait_for_async_insert": 1}))
		if err := AppendServed(ctx, r.conn, rows); err != nil {
			reason := dropInsertFailed
			if exception, ok := errors.AsType[*clickhouse.Exception](err); ok && exception.Code == unknownTableCode {
				reason = dropTableMissing
			}
			r.drop(ctx, reason, len(rows), err)
		}
	})
}

// AppendServed inserts served connections. The table keeps the latest times per connection.
func AppendServed(ctx context.Context, conn driver.Conn, rows []Served) error {
	batch, err := conn.PrepareBatch(ctx, lastServedInsert)
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
