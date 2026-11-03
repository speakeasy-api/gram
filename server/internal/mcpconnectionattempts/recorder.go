package mcpconnectionattempts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	meterScope = "github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"

	meterSteps   = "mcp.connection.steps"
	meterDropped = "mcp.connection.events.dropped"

	insertStatement = `INSERT INTO %s (
		organization_id, project_id, attempt_id, attempt_started_at, event_time,
		server_kind, server_id, member_id, step, step_result, outcome, reason,
		sub_reason, who_fixes, grant_type, is_final, retries, auth_client_id,
		remote_session_issuer_id, user_session_issuer_id, user_session_client_id,
		remote_session_client_id, subject_urn, user_session_id, remote_session_id,
		trace_id, provider_host, app_key, client_family, client_version, repeats)`

	eventsTable = "mcp_connection_events"

	// defaultQueueSize holds about 2 MiB of rows, enough for a long ClickHouse stall.
	defaultQueueSize = 4096

	// defaultMaxBatch keeps one insert small enough to never be slow to send.
	defaultMaxBatch = 500

	// defaultFlushInterval is how long a row waits in memory when traffic is low.
	defaultFlushInterval = time.Second

	// defaultInsertTimeout bounds one insert on a live connection. Dialing during a stall takes the client's dial timeout.
	defaultInsertTimeout = 3 * time.Second

	// drainLogWait bounds the wait for Shutdown's one log line.
	drainLogWait = time.Second

	// defaultRepeatsInterval is how long a burst that stopped waits before its count is written.
	defaultRepeatsInterval = time.Minute

	// dropLogInterval limits logging per reason during an outage.
	dropLogInterval = time.Minute

	// defaultLogsCheckTimeout bounds one logs lookup. Releasing its lock can add up to 5s on the writer.
	defaultLogsCheckTimeout = 500 * time.Millisecond

	// unknownTableCode is ClickHouse's UNKNOWN_TABLE, seen when code ships before its migration.
	unknownTableCode = 60
)

// dropReason says why rows were not written.
type dropReason string

const (
	dropQueueFull    dropReason = "queue_full"
	dropTableMissing dropReason = "table_missing"
	dropInsertFailed dropReason = "insert_failed"
	dropInvalidEvent dropReason = "invalid_event"
	dropShutdown     dropReason = "shutdown"
	dropLogsOff      dropReason = "logs_off"
	dropLogsCheck    dropReason = "logs_check_failed"
	dropPanic        dropReason = "panic"
	dropRateLimited  dropReason = "rate_limited"
	dropQueueReserve dropReason = "queue_reserved"
	dropRepeatsLost  dropReason = "repeats_lost"
)

// shortLabel is the shape of a sub-reason or grant type. It is what keeps a raw
// provider error message out of those columns.
var shortLabel = regexp.MustCompile(`^[a-zA-Z0-9_:.-]{1,128}$`)

// Option adds a detail to a step. Options are safe on zero values.
type Option struct{ apply func(*Row) }

// Subject is the session subject once known. Nil stores empty.
func Subject(subject *urn.SessionSubject) Option {
	return Option{apply: func(row *Row) {
		if subject != nil && !subject.IsZero() {
			row.SubjectURN = subject.String()
		}
	}}
}

// Grant is the OAuth grant type on token steps. A value that is not a short
// identifier is left out and the row is kept.
func Grant(grantType string) Option {
	return Option{apply: func(row *Row) {
		if shortLabel.MatchString(grantType) {
			row.GrantType = grantType
		}
	}}
}

// Detail is a finer label within the reason, such as org_lookup_failed. A value
// that is not a short identifier is left out and the row is kept.
func Detail(subReason string) Option {
	return Option{apply: func(row *Row) {
		if shortLabel.MatchString(subReason) {
			row.SubReason = subReason
		}
	}}
}

// Provider is the provider's issuer URL or host, normalized here.
func Provider(issuer string) Option {
	return Option{apply: func(row *Row) { row.ProviderHost = NormalizeProviderHost(issuer) }}
}

// Member is the gateway member for member-level steps.
func Member(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.MemberID = idString(id) }}
}

// Retries is how many retries this step spent.
func Retries(n uint32) Option {
	return Option{apply: func(row *Row) { row.Retries = n }}
}

// AuthClient is the mcp_auth_clients row.
func AuthClient(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.AuthClientID = idString(id) }}
}

// RemoteSessionIssuer is the upstream provider's remote_session_issuers row.
func RemoteSessionIssuer(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.RemoteSessionIssuerID = idString(id) }}
}

// UserSessionIssuer is the Speakeasy issuer's user_session_issuers row.
func UserSessionIssuer(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.UserSessionIssuerID = idString(id) }}
}

// UserSessionClient is the AI app's user_session_clients row.
func UserSessionClient(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.UserSessionClientID = idString(id) }}
}

// RemoteSessionClient is the remote_session_clients row.
func RemoteSessionClient(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.RemoteSessionClientID = idString(id) }}
}

// UserSession is the user_sessions row.
func UserSession(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.UserSessionID = idString(id) }}
}

// RemoteSession is the remote_sessions row.
func RemoteSession(id uuid.UUID) Option {
	return Option{apply: func(row *Row) { row.RemoteSessionID = idString(id) }}
}

// Row is one mcp_connection_events row.
type Row struct {
	OrganizationID        string
	ProjectID             uuid.UUID
	AttemptID             uuid.UUID
	AttemptStartedAt      time.Time
	EventTime             time.Time
	ServerKind            string
	ServerID              string
	MemberID              string
	Step                  string
	StepResult            string
	Outcome               string
	Reason                string
	SubReason             string
	WhoFixes              string
	GrantType             string
	IsFinal               bool
	Retries               uint32
	AuthClientID          string
	RemoteSessionIssuerID string
	UserSessionIssuerID   string
	UserSessionClientID   string
	RemoteSessionClientID string
	SubjectURN            string
	UserSessionID         string
	RemoteSessionID       string
	TraceID               string
	ProviderHost          string
	AppKey                string
	ClientFamily          string
	ClientVersion         string
	Repeats               uint32
}

var errInvalidEvent = errors.New("invalid connection event")

// RowAt builds a row without recording it, for seed data. Flows use Step, End
// and Once. Outcome is only ever set on a final row.
func RowAt(attempt Attempt, step Step, what Happened, final bool, at time.Time, opts ...Option) (Row, error) {
	var none Row
	if attempt.IsZero() || attempt.startedAt.IsZero() || !attempt.scope.valid() {
		return none, fmt.Errorf("%w: attempt not started or missing tenant fields", errInvalidEvent)
	}
	if step.name == "" {
		return none, fmt.Errorf("%w: no step", errInvalidEvent)
	}

	result, reason := "", ""
	outcome, who := OutcomeConnected, WhoNobody
	switch happened := what.(type) {
	case Result:
		result = happened.name
	case Code:
		meaning, ok := catalog[happened.name]
		if !ok {
			return none, fmt.Errorf("%w: unknown code %q", errInvalidEvent, happened.name)
		}
		result, reason = meaning.result.name, happened.name
		outcome, who = meaning.Outcome, meaning.WhoFixes
	}
	if result == "" {
		return none, fmt.Errorf("%w: nothing happened", errInvalidEvent)
	}

	storedOutcome := ""
	if final {
		storedOutcome = string(outcome)
	}
	appKey := attempt.scope.appKey
	if appKey == "" {
		appKey = AppUnverified.key
	}
	row := Row{
		OrganizationID:        attempt.scope.organizationID,
		ProjectID:             attempt.scope.projectID,
		AttemptID:             attempt.id,
		AttemptStartedAt:      attempt.startedAt,
		EventTime:             at.UTC(),
		ServerKind:            attempt.scope.serverKind,
		ServerID:              idString(attempt.scope.serverID),
		MemberID:              "",
		Step:                  step.name,
		StepResult:            result,
		Outcome:               storedOutcome,
		Reason:                reason,
		SubReason:             "",
		WhoFixes:              string(who),
		GrantType:             "",
		IsFinal:               final,
		Retries:               0,
		AuthClientID:          "",
		RemoteSessionIssuerID: "",
		UserSessionIssuerID:   "",
		UserSessionClientID:   "",
		RemoteSessionClientID: "",
		SubjectURN:            "",
		UserSessionID:         "",
		RemoteSessionID:       "",
		TraceID:               "",
		ProviderHost:          "",
		AppKey:                appKey,
		ClientFamily:          "",
		ClientVersion:         "",
		Repeats:               0,
	}
	for _, opt := range opts {
		if opt.apply != nil {
			opt.apply(&row)
		}
	}
	return row, nil
}

func idString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// AppendRows inserts rows with the posture the context carries. It never
// retries: a repeated insert would double count step_events in the rollup.
func AppendRows(ctx context.Context, conn driver.Conn, rows []Row) error {
	return appendRowsInto(ctx, conn, eventsTable, rows)
}

func appendRowsInto(ctx context.Context, conn driver.Conn, table string, rows []Row) error {
	batch, err := conn.PrepareBatch(ctx, fmt.Sprintf(insertStatement, table))
	if err != nil {
		return fmt.Errorf("prepare connection events: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return batch.Abort() })

	for i := range rows {
		row := &rows[i]
		if err := batch.Append(
			row.OrganizationID, row.ProjectID, row.AttemptID, row.AttemptStartedAt, row.EventTime,
			row.ServerKind, row.ServerID, row.MemberID, row.Step, row.StepResult, row.Outcome, row.Reason,
			row.SubReason, row.WhoFixes, row.GrantType, row.IsFinal, row.Retries, row.AuthClientID,
			row.RemoteSessionIssuerID, row.UserSessionIssuerID, row.UserSessionClientID,
			row.RemoteSessionClientID, row.SubjectURN, row.UserSessionID, row.RemoteSessionID,
			row.TraceID, row.ProviderHost, row.AppKey, row.ClientFamily, row.ClientVersion, row.Repeats,
		); err != nil {
			return fmt.Errorf("append connection event: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert connection events: %w", err)
	}
	return nil
}

// config holds the write path's bounds.
type config struct {
	table            string
	queueSize        int
	maxBatch         int
	flushInterval    time.Duration
	insertTimeout    time.Duration
	logsCheckTimeout time.Duration
	repeatsInterval  time.Duration
}

// Recorder writes connection events. Its methods only queue a row. One goroutine checks the
// logs setting and inserts, never retrying. Rows it can't write drop and count. Nil does nothing.
type Recorder struct {
	logger      *slog.Logger
	conn        driver.Conn
	limiter     *onceLimiter
	logsEnabled func(context.Context, string) (bool, error)
	capture     func(Row)
	cfg         config
	steps       metric.Int64Counter
	dropped     metric.Int64Counter

	queue    chan Row
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	// stopMu makes enqueueing and stopping exclusive, so each row is drained or counted.
	stopMu  sync.RWMutex
	stopped bool

	lastDrop map[dropReason]*atomic.Int64

	// unfinished counts rows accepted but not yet written or dropped, wherever they are.
	unfinished atomic.Int64

	// dropTotals counts rows per reason, so Shutdown can report what its drain lost.
	dropTotals map[dropReason]*atomic.Int64
}

func newDropCounters() map[dropReason]*atomic.Int64 {
	return map[dropReason]*atomic.Int64{
		dropQueueFull:    {},
		dropTableMissing: {},
		dropInsertFailed: {},
		dropInvalidEvent: {},
		dropShutdown:     {},
		dropLogsOff:      {},
		dropLogsCheck:    {},
		dropPanic:        {},
		dropRateLimited:  {},
		dropQueueReserve: {},
		dropRepeatsLost:  {},
	}
}

// NewRecorder starts a recorder. Stop it with Shutdown. Without logsEnabled
// nothing is written.
func NewRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, conn driver.Conn, logsEnabled func(context.Context, string) (bool, error)) *Recorder {
	return newRecorder(logger, meterProvider, conn, logsEnabled, config{
		table:            eventsTable,
		queueSize:        defaultQueueSize,
		maxBatch:         defaultMaxBatch,
		flushInterval:    defaultFlushInterval,
		insertTimeout:    defaultInsertTimeout,
		logsCheckTimeout: defaultLogsCheckTimeout,
		repeatsInterval:  defaultRepeatsInterval,
	})
}

func newRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, conn driver.Conn, logsEnabled func(context.Context, string) (bool, error), cfg config) *Recorder {
	logger = logger.With(attr.SlogComponent("mcp_connection_attempts"))
	meter := meterProvider.Meter(meterScope)

	steps, err := meter.Int64Counter(meterSteps,
		metric.WithDescription("MCP connection attempt steps by outcome, step, reason and who fixes it."),
		metric.WithUnit("{step}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterSteps), attr.SlogError(err))
	}
	dropped, err := meter.Int64Counter(meterDropped,
		metric.WithDescription("MCP connection event rows dropped instead of written, by reason."),
		metric.WithUnit("{row}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterDropped), attr.SlogError(err))
	}

	r := &Recorder{
		logger:      logger,
		conn:        conn,
		limiter:     newOnceLimiter(),
		logsEnabled: logsEnabled,
		capture:     nil,
		cfg:         cfg,
		steps:       steps,
		dropped:     dropped,
		queue:       make(chan Row, cfg.queueSize),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		stopOnce:    sync.Once{},
		stopMu:      sync.RWMutex{},
		stopped:     false,
		lastDrop:    newDropCounters(),
		unfinished:  atomic.Int64{},
		dropTotals:  newDropCounters(),
	}
	go r.run()
	return r
}

// NewCapturing returns a recorder that hands each row to capture instead of
// writing it. Use mcpconnectionattemptstest rather than calling this.
func NewCapturing(capture func(Row), logsEnabled func(context.Context, string) (bool, error)) *Recorder {
	return &Recorder{
		logger:      slog.New(slog.DiscardHandler),
		conn:        nil,
		limiter:     nil,
		logsEnabled: logsEnabled,
		capture:     capture,
		cfg:         config{table: "", queueSize: 0, maxBatch: 0, flushInterval: 0, insertTimeout: 0, logsCheckTimeout: time.Second, repeatsInterval: 0},
		steps:       nil,
		dropped:     nil,
		queue:       nil,
		stop:        nil,
		done:        nil,
		stopOnce:    sync.Once{},
		stopMu:      sync.RWMutex{},
		stopped:     false,
		lastDrop:    newDropCounters(),
		unfinished:  atomic.Int64{},
		dropTotals:  newDropCounters(),
	}
}

// Step records a step that does not end the attempt.
func (r *Recorder) Step(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) {
	r.record(ctx, attempt, step, what, false, false, opts)
}

// End records the step that ends the attempt. Its outcome is Connected for a
// Result and the catalog's outcome for a Code. A flow ends an attempt once.
func (r *Recorder) End(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) {
	r.record(ctx, attempt, step, what, true, false, opts)
}

// Once records an attempt that is one request: one row, final. It has no proof of a real flow
// behind it, so repeats count as rate_limited, and rows past its queue share as queue_reserved.
func (r *Recorder) Once(ctx context.Context, scope Scope, step Step, what Happened, opts ...Option) {
	r.record(ctx, Begin(scope), step, what, true, true, opts)
}

func (r *Recorder) record(ctx context.Context, attempt Attempt, step Step, what Happened, final, limited bool, opts []Option) {
	if r == nil || attempt.IsZero() {
		return
	}
	row, err := RowAt(attempt, step, what, final, time.Now(), opts...)
	if err != nil {
		r.drop(ctx, dropInvalidEvent, 1, err)
		return
	}
	if limited && r.limiter != nil {
		if r.queueReserved() {
			r.dropLostRepeats(ctx, r.limiter.holdBack(row))
			r.drop(ctx, dropQueueReserve, 1, nil)
			return
		}
		decision := r.limiter.take(row, time.Now())
		r.dropLostRepeats(ctx, decision.lost)
		if !decision.allowed {
			r.drop(ctx, dropRateLimited, 1, nil)
			return
		}
		row.Repeats = decision.repeats
	}
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.HasTraceID() {
		row.TraceID = spanCtx.TraceID().String()
	}
	r.countStep(ctx, row)
	if r.capture != nil {
		if reason, _ := r.organizationAllowed(row.OrganizationID); reason == "" {
			r.capture(row)
		}
		return
	}

	if reason := r.enqueue(row); reason != "" {
		r.drop(ctx, reason, 1, nil)
	}
}

// enqueue holds the lock only for a non-blocking send, and reports any drop after releasing it.
func (r *Recorder) enqueue(row Row) dropReason {
	r.stopMu.RLock()
	defer r.stopMu.RUnlock()

	if r.stopped {
		return dropShutdown
	}
	r.unfinished.Add(1)
	select {
	case r.queue <- row:
		return ""
	default:
		r.unfinished.Add(-1)
		return dropQueueFull
	}
}

func (r *Recorder) dropLostRepeats(ctx context.Context, lost int) {
	if lost > 0 {
		r.drop(ctx, dropRepeatsLost, lost, nil)
	}
}

// queueReserved reports that the rest of the queue is kept for rows on real attempts.
func (r *Recorder) queueReserved() bool {
	return len(r.queue)*onceQueueShareDenominator >= cap(r.queue)*onceQueueShareNumerator
}

// Shutdown writes what is queued, each batch once, and stops the goroutine. It logs
// what the drain lost, because metrics may not be exported by then.
func (r *Recorder) Shutdown(ctx context.Context) error {
	if r == nil || r.stop == nil {
		return nil
	}
	first := false
	var before map[dropReason]int64
	r.stopOnce.Do(func() {
		first = true
		r.stopMu.Lock()
		r.stopped = true
		r.stopMu.Unlock()
		before = r.dropSnapshot()
		close(r.stop)
	})

	var err error
	select {
	case <-r.done:
	case <-ctx.Done():
		err = fmt.Errorf("drain connection events: %w", ctx.Err())
	}
	if first {
		// A stuck log handler must not hold Shutdown past its deadline.
		logged := make(chan struct{})
		go func() {
			defer close(logged)
			r.logDrainLosses(context.WithoutCancel(ctx), before)
		}()
		select {
		case <-logged:
		case <-time.After(drainLogWait):
		}
	}
	return err
}

func (r *Recorder) dropSnapshot() map[dropReason]int64 {
	snapshot := make(map[dropReason]int64, len(r.dropTotals))
	for reason, total := range r.dropTotals {
		snapshot[reason] = total.Load()
	}
	return snapshot
}

// logDrainLosses logs one line of rows lost since the drain began. Logs-off rows are not a loss.
func (r *Recorder) logDrainLosses(ctx context.Context, before map[dropReason]int64) {
	var parts []string
	for reason, count := range r.dropSnapshot() {
		if lost := count - before[reason]; lost > 0 && reason != dropLogsOff {
			parts = append(parts, fmt.Sprintf("%s=%d", reason, lost))
		}
	}
	if unfinished := r.unfinished.Load(); unfinished > 0 {
		parts = append(parts, fmt.Sprintf("unfinished=%d", unfinished))
	}
	if len(parts) == 0 {
		return
	}
	slices.Sort(parts)
	r.logger.WarnContext(ctx, "connection events lost during shutdown", attr.SlogConnectionDropCounts(strings.Join(parts, " ")))
}

func (r *Recorder) run() {
	defer close(r.done)

	ticker := time.NewTicker(r.cfg.flushInterval)
	defer ticker.Stop()
	repeats := time.NewTicker(r.cfg.repeatsInterval)
	defer repeats.Stop()

	// organizations holds each logs check for one flush: one lookup per organization per interval.
	organizations := map[string]dropReason{}
	batch := make([]Row, 0, r.cfg.maxBatch)
	flush := func() {
		clear(organizations)
		rows := batch
		batch = make([]Row, 0, r.cfg.maxBatch)
		if len(rows) > 0 {
			r.surviving(len(rows), func() { r.insert(rows) })
			r.unfinished.Add(-int64(len(rows)))
		}
		// A tick from during a slow insert would flush a nearly empty batch and let a backlog grow.
		ticker.Reset(r.cfg.flushInterval)
	}
	add := func(row Row) {
		pending := len(batch)
		r.surviving(1, func() {
			reason, checked := organizations[row.OrganizationID]
			if !checked {
				var err error
				reason, err = r.organizationAllowed(row.OrganizationID)
				organizations[row.OrganizationID] = reason
				if err != nil {
					r.drop(context.Background(), reason, 1, err)
					return
				}
			}
			if reason != "" {
				r.drop(context.Background(), reason, 1, nil)
				return
			}
			batch = append(batch, row)
		})
		if len(batch) == pending {
			r.unfinished.Add(-1)
		}
		if len(batch) >= r.cfg.maxBatch {
			flush()
		}
	}
	addPending := func(rows []Row) {
		for _, row := range rows {
			r.unfinished.Add(1)
			r.countStep(context.Background(), row)
			add(row)
		}
	}
	for {
		select {
		case row := <-r.queue:
			add(row)
		case <-ticker.C:
			flush()
		case <-repeats.C:
			addPending(r.limiter.pendingRows(time.Now(), false))
		case <-r.stop:
			for {
				select {
				case row := <-r.queue:
					add(row)
				default:
					addPending(r.limiter.pendingRows(time.Now(), true))
					flush()
					return
				}
			}
		}
	}
}

// surviving runs fn and counts its rows as dropped if it panics.
func (r *Recorder) surviving(rows int, fn func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.drop(context.Background(), dropPanic, rows, fmt.Errorf("connection events writer: %v", recovered))
		}
	}()
	fn()
}

// organizationAllowed is the one place that decides whether an organization's attempts
// are stored. It returns why not, or "" to store them. A check that fails means do not store.
func (r *Recorder) organizationAllowed(organizationID string) (dropReason, error) {
	if r.logsEnabled == nil {
		return dropLogsOff, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.logsCheckTimeout)
	defer cancel()

	enabled, err := r.logsEnabled(ctx, organizationID)
	switch {
	case err != nil:
		return dropLogsCheck, fmt.Errorf("check logs setting: %w", err)
	case !enabled:
		return dropLogsOff, nil
	default:
		return "", nil
	}
}

// insert writes one batch once and waits for the commit, so failures are counted.
func (r *Recorder) insert(rows []Row) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.insertTimeout)
	defer cancel()

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1, "wait_for_async_insert": 1}))
	err := appendRowsInto(ctx, r.conn, r.cfg.table, rows)
	if err == nil {
		return
	}
	reason := dropInsertFailed
	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok && exception.Code == unknownTableCode {
		reason = dropTableMissing
	}
	r.drop(ctx, reason, len(rows), err)
}

func (r *Recorder) countStep(ctx context.Context, row Row) {
	if r.steps == nil {
		return
	}
	r.steps.Add(ctx, 1, metric.WithAttributes(
		attr.ConnectionOutcome(row.Outcome),
		attr.ConnectionStep(row.Step),
		attr.ConnectionReason(row.Reason),
		attr.ConnectionWhoFixes(row.WhoFixes),
	))
}

func (r *Recorder) drop(ctx context.Context, reason dropReason, rows int, cause error) {
	r.dropTotals[reason].Add(int64(rows))
	if r.dropped != nil {
		r.dropped.Add(ctx, int64(rows), metric.WithAttributes(attr.ConnectionDropReason(string(reason))))
	}
	// Expected for any organization with logs off, so it is only counted.
	if reason == dropLogsOff {
		return
	}
	last := r.lastDrop[reason]
	now := time.Now().UnixNano()
	previous := last.Load()
	if now-previous < int64(dropLogInterval) || !last.CompareAndSwap(previous, now) {
		return
	}
	attrs := []any{attr.SlogConnectionDropReason(string(reason))}
	if cause != nil {
		attrs = append(attrs, attr.SlogError(cause))
	}
	r.logger.WarnContext(ctx, "dropped connection events", attrs...)
}
