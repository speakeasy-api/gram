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
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	meterScope = "github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"

	meterSteps   = "mcp.connection.steps"
	meterDropped = "mcp.connection.events.dropped"

	meterReferenceTimeouts = "mcp.connection.references.timed_out"

	insertStatement = `INSERT INTO %s (
		organization_id, project_id, attempt_id, attempt_started_at, event_time,
		server_kind, server_id, member_id, step, step_result, outcome, reason,
		sub_reason, who_fixes, grant_type, is_final, retries, auth_client_id,
		remote_session_issuer_id, user_session_issuer_id, user_session_client_id,
		remote_session_client_id, subject_urn, user_session_id, remote_session_id,
		trace_id, provider_host, app_key, client_family, client_version, repeats, device_key)`

	eventsTable = "mcp_connection_events"

	// defaultQueueSize holds about 2 MiB of rows, enough for a long ClickHouse stall.
	defaultQueueSize = 4096

	// defaultMaxBatch keeps one insert small enough to never be slow to send.
	defaultMaxBatch = 500

	// defaultFlushInterval is how long a row waits in memory when traffic is low.
	defaultFlushInterval = time.Second

	// defaultInsertTimeout bounds one insert on a live connection. Dialing during a stall takes the client's dial timeout.
	defaultInsertTimeout = 3 * time.Second

	// drainLogWait bounds the wait for each shutdown log line.
	drainLogWait = time.Second

	// writerJoinMargin is the part of ShutdownAndWait's deadline kept for cancelling the
	// writer and joining it. Cancelled I/O returns at once; the rest is headroom.
	writerJoinMargin = 2 * time.Second

	// minDrainWindow is the least drain time ShutdownAndWait keeps from a budget too small
	// for writerJoinMargin, so a short budget still drains a healthy backlog.
	minDrainWindow = 500 * time.Millisecond

	// writerJoinGrace is added to the writer's longest operation for the wait on a cancelled
	// writer. A cancelled insert returns at once; this bounds a driver that ignores cancellation.
	writerJoinGrace = time.Second

	// logsCheckLockRelease is what releasing the feature cache lock can add to a logs check on
	// the writer. The release is detached from cancellation, so the join must outlast it.
	logsCheckLockRelease = 5 * time.Second

	// defaultRepeatsInterval is how long a burst that stopped waits before its count is written.
	defaultRepeatsInterval = time.Minute

	// dropLogInterval limits logging per reason during an outage.
	dropLogInterval = time.Minute

	// defaultLogsCheckTimeout bounds one logs lookup. Releasing its lock can add logsCheckLockRelease on the writer.
	defaultLogsCheckTimeout = 500 * time.Millisecond

	// referenceLogsTimeout is how long a failure waits for its logs answer; a slower one shows no reference.
	referenceLogsTimeout = 100 * time.Millisecond

	// referenceLogsOffTTL is how long a logs-off answer serves references. Staying stale only hides a reference.
	referenceLogsOffTTL = 30 * time.Second

	// referenceLogsOnTTL is how long a logs-on answer serves references. It matches the writer's own
	// per-flush answer, so a reference outlives a logs-off change no longer than a queued row does.
	referenceLogsOnTTL = defaultFlushInterval

	// unknownTableCode is ClickHouse's UNKNOWN_TABLE, seen when code ships before its migration.
	unknownTableCode = 60
)

// dropReason says why events were not written. rate_limited and queue_reserved apply only to Start and Served rows.
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

// RequestMethod labels the request a row is about. It is the sub-reason, and
// Once folds repeats per method. The label must come from a fixed set.
func RequestMethod(label string) Option {
	return Option{apply: func(row *Row) {
		if shortLabel.MatchString(label) {
			row.SubReason = label
			row.foldMethod = label
		}
	}}
}

// FoldRefusedSubjects folds a refused Once row's repeats across every subject that shares key,
// for subjects anyone can mint, such as a public workload issuer's. A row that also stands for
// held-back rows stores no subject, since theirs may differ. Successes still fold per subject.
func FoldRefusedSubjects(key string) Option {
	return Option{apply: func(row *Row) {
		if row.Reason != "" && shortLabel.MatchString(key) {
			row.foldSubjects = key
		}
	}}
}

// dropFoldedSubject clears the subject of a row folded across subjects that stands for others too.
func (row *Row) dropFoldedSubject() {
	if row.foldSubjects != "" {
		row.SubjectURN = ""
	}
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

// FailedElsewhere marks a row whose failure was not at the provider host it names, so the export leaves the host out.
func FailedElsewhere() Option {
	return Option{apply: func(row *Row) { row.failedElsewhere = true }}
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
	DeviceKey             string

	// client waits here for the writer to read it. It is never stored or logged.
	client requestClient

	// foldMethod separates Once repeats by request method. It is never stored.
	foldMethod string

	// foldSubjects replaces the subject when folding Once repeats. It is never stored.
	foldSubjects string

	// failedElsewhere is for the export only and is never stored.
	failedElsewhere bool
}

var errInvalidEvent = errors.New("invalid connection event")

var errWriterStillRunning = errors.New("connection events writer still running")

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
		DeviceKey:             "",
		client:                requestClient{present: false, userAgent: "", ipAddress: "", appFamily: attempt.scope.appFamily, reported: nil},
		foldMethod:            "",
		foldSubjects:          "",
		failedElsewhere:       false,
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
			row.TraceID, row.ProviderHost, row.AppKey, row.ClientFamily, row.ClientVersion, row.Repeats, row.DeviceKey,
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
	served      *servedConnections
	logsEnabled func(context.Context, string) (bool, error)
	deviceKeys  DeriveKey
	exporter    *exporter
	capture     func(Row)
	cfg         config
	steps       metric.Int64Counter
	dropped     metric.Int64Counter

	// referenceTimeouts counts references not shown because the logs check outran its wait.
	referenceTimeouts metric.Int64Counter

	queue        chan Row
	stop         chan struct{}
	done         chan struct{}
	stopOnce     sync.Once
	writerCtx    context.Context //nolint:containedctx // Recorder-owned background lifecycle; shutdown cancels writer IO, never a request context.
	cancelWriter context.CancelFunc

	// stopMu makes enqueueing and stopping exclusive, so each row is drained or counted.
	stopMu  sync.RWMutex
	stopped bool

	lastDrop map[dropReason]*atomic.Int64

	// unfinished counts rows accepted but not yet written or dropped, wherever they are.
	unfinished atomic.Int64

	// dropTotals counts rows per reason, so Shutdown can report what its drain lost.
	dropTotals map[dropReason]*atomic.Int64

	// insertFailing is set while the last insert failed, so references stop promising rows.
	insertFailing atomic.Bool

	// referableOrgs remembers each organization's logs answer for references. Nil checks every time.
	referableOrgs *simplelru.LRU[string, referableOrg]

	// referableMu guards referableOrgs. It is never held during a logs check.
	referableMu sync.Mutex

	// referenceChecks runs one background logs check per organization, so a slow one holds no request up.
	referenceChecks singleflight.Group
}

// referableOrg is one logs answer and when it was read.
type referableOrg struct {
	allowed bool
	at      time.Time
}

func (o referableOrg) ttl() time.Duration {
	if o.allowed {
		return referenceLogsOnTTL
	}
	return referenceLogsOffTTL
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
func NewRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, conn driver.Conn, logsEnabled func(context.Context, string) (bool, error), opts ...RecorderOption) *Recorder {
	return newRecorder(logger, meterProvider, conn, logsEnabled, config{
		table:            eventsTable,
		queueSize:        defaultQueueSize,
		maxBatch:         defaultMaxBatch,
		flushInterval:    defaultFlushInterval,
		insertTimeout:    defaultInsertTimeout,
		logsCheckTimeout: defaultLogsCheckTimeout,
		repeatsInterval:  defaultRepeatsInterval,
	}, opts...)
}

func newRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, conn driver.Conn, logsEnabled func(context.Context, string) (bool, error), cfg config, opts ...RecorderOption) *Recorder {
	logger = logger.With(attr.SlogComponent("mcp_connection_attempts"))
	meter := meterProvider.Meter(meterScope)

	steps, err := meter.Int64Counter(meterSteps,
		metric.WithDescription("MCP connection attempt steps stored in ClickHouse, by outcome, step, reason, who fixes it and known provider. Rows not stored, including those of organizations with logs off, are not counted."),
		metric.WithUnit("{step}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterSteps), attr.SlogError(err))
	}
	dropped, err := meter.Int64Counter(meterDropped,
		metric.WithDescription("MCP connection events lost instead of written, by reason. A row with repeats counts once per event it stands for."),
		metric.WithUnit("{event}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterDropped), attr.SlogError(err))
	}
	referenceTimeouts, err := meter.Int64Counter(meterReferenceTimeouts,
		metric.WithDescription("MCP connection references not shown because the logs check was slower than its wait."),
		metric.WithUnit("{reference}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterReferenceTimeouts), attr.SlogError(err))
	}

	writerCtx, cancelWriter := context.WithCancel(context.Background())
	r := &Recorder{
		logger:      logger,
		conn:        conn,
		limiter:     newOnceLimiter(),
		served:      newServedConnections(),
		logsEnabled: logsEnabled,
		deviceKeys:  nil,
		exporter:    nil,
		capture:     nil,
		cfg:         cfg,
		steps:       steps,
		dropped:     dropped,

		referenceTimeouts: referenceTimeouts,
		queue:             make(chan Row, cfg.queueSize),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
		stopOnce:          sync.Once{},
		writerCtx:         writerCtx,
		cancelWriter:      cancelWriter,
		stopMu:            sync.RWMutex{},
		stopped:           false,
		lastDrop:          newDropCounters(),
		unfinished:        atomic.Int64{},
		dropTotals:        newDropCounters(),

		insertFailing: atomic.Bool{},
		referableOrgs: newReferableOrgs(),
		referableMu:   sync.Mutex{},

		referenceChecks: singleflight.Group{},
	}
	for _, opt := range opts {
		opt(r)
	}
	r.startExport(meter)
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
		served:      newServedConnections(),
		logsEnabled: logsEnabled,
		deviceKeys:  nil,
		exporter:    nil,
		capture:     capture,
		cfg:         config{table: "", queueSize: 0, maxBatch: 0, flushInterval: 0, insertTimeout: 0, logsCheckTimeout: time.Second, repeatsInterval: 0},
		steps:       nil,
		dropped:     nil,

		referenceTimeouts: nil,
		queue:             nil,
		stop:              nil,
		done:              nil,
		stopOnce:          sync.Once{},
		writerCtx:         context.Background(),
		cancelWriter:      nil,
		stopMu:            sync.RWMutex{},
		stopped:           false,
		lastDrop:          newDropCounters(),
		unfinished:        atomic.Int64{},
		dropTotals:        newDropCounters(),

		insertFailing: atomic.Bool{},
		referableOrgs: nil,
		referableMu:   sync.Mutex{},

		referenceChecks: singleflight.Group{},
	}
}

func newReferableOrgs() *simplelru.LRU[string, referableOrg] {
	orgs, err := simplelru.NewLRU[string, referableOrg](onceMaxOrgs, nil)
	if err != nil {
		// Only a non-positive size fails, and this is a constant.
		panic("connection reference cache size must be positive")
	}
	return orgs
}

// limit is how much a row is held back before it is queued.
type limit int

const (
	// limitNone is for rows on an attempt a caller already proved it started.
	limitNone limit = iota
	// limitStart is the per-organization ceiling and queue share, for a step anyone can start.
	limitStart
	// limitOnce adds the per-key limit and repeats count to limitStart.
	limitOnce
	// limitServed keeps one row per connection per servedInterval, within limitStart's limits, and drops the rest.
	limitServed
)

// Start records the first step of an attempt that any caller can begin, such as /authorize.
// Nothing proves a real flow yet, so it takes Once's per-organization ceiling and queue share.
func (r *Recorder) Start(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) {
	r.record(ctx, attempt, step, what, false, limitStart, opts)
}

// Step records a step that does not end the attempt. It reports capture/queue acceptance.
func (r *Recorder) Step(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) bool {
	return r.record(ctx, attempt, step, what, false, limitNone, opts)
}

// End records the step that ends the attempt. Its outcome is Connected for a
// Result and the catalog's outcome for a Code. A flow ends an attempt once.
func (r *Recorder) End(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) {
	r.record(ctx, attempt, step, what, true, limitNone, opts)
}

// Once records an attempt that is one request: one row, final. It has no proof of a real flow
// behind it, so repeats and rows past its queue share are held back and written later as a
// count; a held key evicted before then is lost as repeats_lost. It reports capture/queue
// acceptance, not eventual persistence; held-back and rejected rows return false. A held-back
// row still leaves a reference: every row folded together is stored under one id, the first's.
// That row holds one held request's details, so its id names the failure, not which request
// it was; rows with no known subject leave none, since different callers fold together.
//
// ResultServed is sampled instead: at most one row per person, server or gateway member, and
// app per hour in this process, none for a caller with no subject or an anonymous one, and no
// count of the rest. Readers of attempts leave these rows out.
func (r *Recorder) Once(ctx context.Context, scope Scope, step Step, what Happened, opts ...Option) bool {
	if what == ResultServed {
		return r.record(ctx, Begin(scope), step, what, true, limitServed, opts)
	}
	return r.record(ctx, Begin(scope), step, what, true, limitOnce, opts)
}

func (r *Recorder) record(ctx context.Context, attempt Attempt, step Step, what Happened, final bool, held limit, opts []Option) (accepted bool) {
	if _, failed := what.(Code); failed {
		MarkFailed(ctx)
	}
	if r == nil || attempt.IsZero() {
		return false
	}
	var row Row
	referenced := false
	// Runs last: a failure that leaves no id of its own still hides an earlier one.
	defer func() {
		if !referenced {
			r.leaveReference(ctx, step, row.OrganizationID, uuid.Nil, what)
		}
	}()
	// Recording must never panic a request.
	defer func() {
		if recovered := recover(); recovered != nil {
			r.drop(ctx, dropPanic, events(row), fmt.Errorf("connection events record: %v", recovered))
			accepted = false
		}
	}()
	row, err := RowAt(attempt, step, what, final, time.Now(), opts...)
	if err != nil {
		r.drop(ctx, dropInvalidEvent, 1, err)
		return false
	}
	// Before the limits, so a held-back row written later keeps its request's trace.
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.HasTraceID() {
		row.TraceID = spanCtx.TraceID().String()
	}
	var servedAt time.Time
	if held == limitServed {
		if !nameable(row.SubjectURN) {
			return false
		}
		servedAt = time.Now()
		if !r.served.claim(row, servedAt) {
			return false
		}
		defer func() {
			if !accepted {
				r.served.release(row, servedAt)
			}
		}()
	}
	switch {
	case r.limiter == nil:
	case held == limitStart, held == limitServed:
		if r.queueReserved() {
			r.drop(ctx, dropQueueReserve, 1, nil)
			return false
		}
		if !r.limiter.allowOrganization(row.OrganizationID, time.Now()) {
			r.drop(ctx, dropRateLimited, 1, nil)
			return false
		}
	case held == limitOnce:
		decision, stopped := r.admitOnce(row)
		if stopped {
			r.drop(ctx, dropShutdown, 1, nil)
			return false
		}
		r.dropLostRepeats(ctx, decision.lost)
		if decision.repeats > 0 {
			row.dropFoldedSubject()
		}
		if !decision.allowed {
			// Callers with no known subject fold together, so the shared id isn't this caller's.
			if onceKeyOf(row).subject != "" {
				r.leaveReference(ctx, step, row.OrganizationID, decision.foldID, what)
				referenced = true
			}
			return false
		}
		row.Repeats = decision.repeats
		if decision.foldID != uuid.Nil {
			row.AttemptID = decision.foldID
		}
	}
	if r.capture != nil {
		if reason, _ := r.organizationAllowed(row.OrganizationID, r.cfg.logsCheckTimeout); reason == "" {
			r.stampClient(&row)
			r.capture(row)
			r.leaveReference(ctx, step, row.OrganizationID, row.AttemptID, what)
			referenced = true
			return true
		}
		return false
	}

	if reason := r.enqueue(row); reason != "" {
		r.drop(ctx, reason, events(row), nil)
		return false
	}
	r.leaveReference(ctx, step, row.OrganizationID, row.AttemptID, what)
	referenced = true
	return true
}

// leaveReference puts a failure's id on ctx if a row can honestly be expected for it. The row is
// still in memory at this point, so this rules out only what is knowable now: logs off, and a
// ClickHouse that refused the last insert. A check that fails shows nothing.
func (r *Recorder) leaveReference(ctx context.Context, step Step, organizationID string, id uuid.UUID, what Happened) {
	if step == StepDiscoveryMember || !contextvalues.ConnectionReferenceWanted(ctx) {
		return
	}
	worth := FailureWorthAReference(what)
	if !worth && step != StepProviderRefresh {
		return
	}
	reference := ""
	if worth && id != uuid.Nil && !r.insertFailing.Load() && r.logsAllowForReference(ctx, organizationID) {
		reference = id.String()
	}
	if step == StepProviderRefresh {
		if _, failed := what.(Code); failed {
			contextvalues.SetRefreshConnectionReference(ctx, reference)
		}
		return
	}
	contextvalues.SetConnectionReference(ctx, reference)
}

// FailureWorthAReference is true for a failure the person can ask about. A refresh that worked or
// a person's own decline isn't one.
func FailureWorthAReference(what Happened) bool {
	code, ok := what.(Code)
	if !ok {
		return false
	}
	meaning := catalog[code.name]
	return meaning.Outcome == OutcomeFailed || meaning.Outcome == OutcomeBlocked
}

// referenceLogsAnswer asks the logs setting off the writer's and record's panic guards, so it has
// its own: a logs check that panics shows no reference.
func (r *Recorder) referenceLogsAnswer(organizationID string) (allowed bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.drop(context.Background(), dropPanic, 1, fmt.Errorf("connection reference logs check: %v", recovered))
			allowed = false
		}
	}()
	reason, _ := r.organizationAllowed(organizationID, r.cfg.logsCheckTimeout)
	return reason == ""
}

// logsAllowForReference waits a bounded time for one background logs check per organization.
// A slow check answers false now and fills the cache for the next failure.
func (r *Recorder) logsAllowForReference(ctx context.Context, organizationID string) bool {
	if r.referableOrgs == nil {
		return r.referenceLogsAnswer(organizationID)
	}
	r.referableMu.Lock()
	cached, ok := r.referableOrgs.Get(organizationID)
	r.referableMu.Unlock()
	if ok && time.Since(cached.at) < cached.ttl() {
		return cached.allowed
	}
	answer := r.referenceChecks.DoChan(organizationID, func() (any, error) {
		allowed := r.referenceLogsAnswer(organizationID)
		r.referableMu.Lock()
		r.referableOrgs.Add(organizationID, referableOrg{allowed: allowed, at: time.Now()})
		r.referableMu.Unlock()
		return allowed, nil
	})
	timer := time.NewTimer(referenceLogsTimeout)
	defer timer.Stop()
	select {
	case result := <-answer:
		allowed, _ := result.Val.(bool)
		return allowed
	case <-timer.C:
		if r.referenceTimeouts != nil {
			r.referenceTimeouts.Add(ctx, 1)
		}
		return false
	case <-ctx.Done():
		return false
	}
}

// admitOnce applies the Once limits under stopMu, so a row held back before stopping is in the final pass.
func (r *Recorder) admitOnce(row Row) (decision onceDecision, stopped bool) {
	r.stopMu.RLock()
	defer r.stopMu.RUnlock()

	if r.stopped {
		return onceDecision{allowed: false, repeats: 0, lost: 0, foldID: uuid.Nil}, true
	}
	if r.queueReserved() {
		return r.limiter.holdBack(row), false
	}
	return r.limiter.take(row, time.Now()), false
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

// events is how many recorded events rows stand for: each row plus its repeats.
func events(rows ...Row) int {
	total := 0
	for _, row := range rows {
		total += 1 + int(row.Repeats)
	}
	return total
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
// what the drain lost, because metrics may not be exported by then. A drain cut off by
// ctx leaves the writer running; use ShutdownAndWait where its datastore closes next.
func (r *Recorder) Shutdown(ctx context.Context) error {
	if r == nil || r.stop == nil {
		return nil
	}
	first, before, err := r.stopAndDrain(ctx)
	if first {
		logBounded(func() { r.logDrainLosses(context.WithoutCancel(ctx), before) })
	}
	return err
}

// ShutdownAndWait drains until shortly before ctx ends, then cancels in-flight I/O,
// counts the backlog as dropped for shutdown, and joins the writer so its datastore can
// close. The join outlasts ctx, bounded by writerJoinWait. The logs callback and
// datastore driver must honor their operation contexts. Connection log exports still
// waiting for an ack stop waiting once the writer is cancelled.
func (r *Recorder) ShutdownAndWait(ctx context.Context) error {
	if r == nil || r.stop == nil {
		return nil
	}
	drainCtx := ctx
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		drainCtx, cancel = context.WithTimeout(ctx, drainWindow(time.Until(deadline)))
		defer cancel()
	}
	first, before, err := r.stopAndDrain(drainCtx)
	r.cancelWriter()
	// The caller closes ClickHouse next, so wait for the writer even past ctx.
	grace := writerJoinWait(r.cfg.insertTimeout, r.cfg.logsCheckTimeout)
	select {
	case <-r.done:
	case <-time.After(grace):
		err = errors.Join(err, errWriterStillRunning)
		stillRunning := err
		logBounded(func() {
			r.logger.ErrorContext(context.WithoutCancel(ctx), "connection events writer still running after shutdown", attr.SlogError(stillRunning))
		})
	}
	if first {
		logBounded(func() { r.logDrainLosses(context.WithoutCancel(ctx), before) })
	}
	return err
}

// drainWindow is how much of the remaining budget the drain gets: all but writerJoinMargin,
// and never less than minDrainWindow or the whole budget when that is smaller.
func drainWindow(remaining time.Duration) time.Duration {
	return max(remaining-writerJoinMargin, min(remaining, minDrainWindow))
}

// writerJoinWait bounds the wait on a cancelled writer: its longest single operation, an
// insert or a logs check with its lock release, plus writerJoinGrace.
func writerJoinWait(insertTimeout, logsCheckTimeout time.Duration) time.Duration {
	return max(insertTimeout, logsCheckTimeout+logsCheckLockRelease) + writerJoinGrace
}

// MaxShutdownOverrun is the longest ShutdownAndWait can run past its ctx deadline with the
// default timeouts: the writer join plus both bounded shutdown logs.
const MaxShutdownOverrun = max(defaultInsertTimeout, defaultLogsCheckTimeout+logsCheckLockRelease) + writerJoinGrace + 2*drainLogWait

// stopAndDrain stops admission once and waits for the writer to drain until ctx ends.
// first is true for the call that stopped it, with the drop totals from that moment.
func (r *Recorder) stopAndDrain(ctx context.Context) (first bool, before map[dropReason]int64, err error) {
	r.stopOnce.Do(func() {
		first = true
		r.stopMu.Lock()
		r.stopped = true
		r.stopMu.Unlock()
		before = r.dropSnapshot()
		close(r.stop)
	})

	select {
	case <-r.done:
	case <-ctx.Done():
		err = fmt.Errorf("drain connection events: %w", ctx.Err())
	}
	return first, before, err
}

// logBounded runs a shutdown log without letting a stuck log handler hold shutdown past drainLogWait.
func logBounded(log func()) {
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		log()
	}()
	select {
	case <-logged:
	case <-time.After(drainLogWait):
	}
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
	defer r.cancelWriter()
	defer r.stopExport()

	ticker := time.NewTicker(r.cfg.flushInterval)
	defer ticker.Stop()
	repeats := time.NewTicker(r.cfg.repeatsInterval)
	defer repeats.Stop()

	// organizations holds each logs check for one flush: one lookup per organization per interval.
	organizations := map[string]dropReason{}
	// After one failed insert once stopping, the rest drop, so a stalled ClickHouse can't hold shutdown.
	drainFailed := false
	stopping := func() bool {
		select {
		case <-r.stop:
			return true
		default:
			return false
		}
	}
	batch := make([]Row, 0, r.cfg.maxBatch)
	flush := func() {
		clear(organizations)
		rows := batch
		batch = make([]Row, 0, r.cfg.maxBatch)
		if len(rows) > 0 {
			r.surviving(events(rows...), func() {
				switch {
				case r.insert(rows):
					r.countSteps(rows)
					r.exportRows(rows)
				case stopping():
					drainFailed = true
				}
			})
			r.unfinished.Add(-int64(len(rows)))
		}
		// A tick from during a slow insert would flush a nearly empty batch and let a backlog grow.
		ticker.Reset(r.cfg.flushInterval)
	}
	add := func(row Row) {
		if drainFailed || r.writerCtx.Err() != nil {
			r.drop(context.Background(), dropShutdown, events(row), nil)
			r.unfinished.Add(-1)
			return
		}
		pending := len(batch)
		r.surviving(events(row), func() {
			reason, checked := organizations[row.OrganizationID]
			if !checked {
				var err error
				reason, err = r.organizationAllowed(row.OrganizationID, r.cfg.logsCheckTimeout)
				organizations[row.OrganizationID] = reason
				if err != nil {
					r.drop(context.Background(), reason, events(row), err)
					return
				}
			}
			if reason != "" {
				r.drop(context.Background(), reason, events(row), nil)
				return
			}
			r.stampClient(&row)
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
			if row.Repeats > 0 {
				row.dropFoldedSubject()
			}
			r.unfinished.Add(1)
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

// surviving runs fn and counts its n events as dropped if it panics.
func (r *Recorder) surviving(n int, fn func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.drop(context.Background(), dropPanic, n, fmt.Errorf("connection events writer: %v", recovered))
		}
	}()
	fn()
}

// organizationAllowed is the one place that decides whether an organization's attempts
// are stored. It returns why not, or "" to store them. A check that fails means do not store.
func (r *Recorder) organizationAllowed(organizationID string, timeout time.Duration) (dropReason, error) {
	if r.logsEnabled == nil {
		return dropLogsOff, nil
	}
	ctx, cancel := context.WithTimeout(r.writerCtx, timeout)
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

// insert writes one batch once and waits for the commit, so failures are counted. It reports success.
func (r *Recorder) insert(rows []Row) bool {
	if r.writerCtx.Err() != nil {
		r.drop(context.Background(), dropShutdown, events(rows...), nil)
		return false
	}
	ctx, cancel := context.WithTimeout(r.writerCtx, r.cfg.insertTimeout)
	defer cancel()

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1, "wait_for_async_insert": 1}))
	err := appendRowsInto(ctx, r.conn, r.cfg.table, rows)
	r.insertFailing.Store(err != nil)
	if err == nil {
		return true
	}
	reason := dropInsertFailed
	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok && exception.Code == unknownTableCode {
		reason = dropTableMissing
	}
	r.drop(ctx, reason, events(rows...), err)
	return false
}

// countSteps counts stored rows only, so dropped rows, including every row of an
// organization with logs off, never reach the metric.
func (r *Recorder) countSteps(rows []Row) {
	if r.steps == nil {
		return
	}
	for _, row := range rows {
		tags := []attribute.KeyValue{
			attr.ConnectionOutcome(row.Outcome),
			attr.ConnectionStep(row.Step),
			attr.ConnectionReason(row.Reason),
			attr.ConnectionWhoFixes(row.WhoFixes),
		}
		if provider := ProviderLabel(row.ProviderHost); provider != "" {
			tags = append(tags, attr.ConnectionProvider(provider))
		}
		r.steps.Add(context.Background(), 1, metric.WithAttributes(tags...))
	}
}

func (r *Recorder) drop(ctx context.Context, reason dropReason, n int, cause error) {
	r.dropTotals[reason].Add(int64(n))
	if r.dropped != nil {
		r.dropped.Add(ctx, int64(n), metric.WithAttributes(attr.ConnectionDropReason(string(reason))))
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
