package connectionevents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	meterScope = "github.com/speakeasy-api/gram/server/internal/connectionevents"

	meterSteps   = "mcp.connection.steps"
	meterDropped = "mcp.connection.events.dropped"

	insertStatement = `INSERT INTO %s (
		organization_id, project_id, attempt_id, attempt_started_at, event_time,
		server_kind, server_id, member_id, step, step_result, outcome, reason,
		sub_reason, who_fixes, grant_type, is_final, retries, auth_client_id,
		remote_session_issuer_id, user_session_issuer_id, user_session_client_id,
		remote_session_client_id, subject_urn, user_session_id, remote_session_id,
		trace_id, provider_host, app_key)`

	eventsTable = "mcp_connection_events"

	// defaultQueueSize holds about 2 MiB of rows. Sign-ins arrive at a few per
	// second, so this covers a long ClickHouse stall before rows drop.
	defaultQueueSize = 4096

	// defaultMaxBatch keeps one insert small enough to never be slow to send.
	defaultMaxBatch = 500

	// defaultFlushInterval is how long a row waits in memory when traffic is low.
	defaultFlushInterval = time.Second

	// defaultInsertTimeout bounds one insert, so a stalled ClickHouse costs
	// queued rows and never a growing pile of goroutines.
	defaultInsertTimeout = 3 * time.Second

	// dropLogInterval limits drop logging during an outage, when every flush fails.
	dropLogInterval = time.Minute

	// defaultFeatureCheckTimeout bounds one organization's logs check. A cache miss
	// can take a lock and query Postgres. A slow check delays the queue, never a
	// request, and then that organization's rows are not written.
	defaultFeatureCheckTimeout = 500 * time.Millisecond

	// endingCheckTimeout bounds the check that an attempt has not already ended.
	// A slow check costs a queue delay, never a request, and then fails open.
	endingCheckTimeout = 200 * time.Millisecond

	// endedAttemptTTL outlives an attempt: a browser attempt can stay live about
	// 50 minutes, and readers wait 90 before calling one abandoned.
	endedAttemptTTL = 2 * time.Hour

	// unknownTableCode is ClickHouse's UNKNOWN_TABLE: code rolled out before the
	// migration that adds the table.
	unknownTableCode = 60
)

// DropReason says why rows were not written.
type DropReason string

const (
	DropQueueFull    DropReason = "queue_full"
	DropTableMissing DropReason = "table_missing"
	DropInsertFailed DropReason = "insert_failed"
	DropInvalidEvent DropReason = "invalid_event"
	DropShutdown     DropReason = "shutdown"
	DropDuplicateEnd DropReason = "duplicate_ending"
	DropLogsOff      DropReason = "logs_off"
	DropLogsCheck    DropReason = "logs_check_failed"
)

// shortLabel is the shape of a sub-reason or grant type. It is what keeps a raw
// provider error message out of those columns.
var shortLabel = regexp.MustCompile(`^[a-zA-Z0-9_:.-]{1,128}$`)

// Option adds a detail to a step. Options are safe on nil and zero values.
type Option func(*Row)

// Subject is the session subject once known. Nil stores empty.
func Subject(subject *urn.SessionSubject) Option {
	return func(row *Row) {
		if subject != nil && !subject.IsZero() {
			row.SubjectURN = subject.String()
		}
	}
}

// Provider is the provider's issuer URL or host, normalized here.
func Provider(issuer string) Option {
	return func(row *Row) { row.ProviderHost = NormalizeProviderHost(issuer) }
}

// Member is the gateway member for member-level steps.
func Member(id uuid.UUID) Option {
	return func(row *Row) { row.MemberID = idString(id) }
}

// Grant is the OAuth grant type on token steps. A value that is not a short
// identifier is left out and the row is kept.
func Grant(grantType string) Option {
	return func(row *Row) {
		if shortLabel.MatchString(grantType) {
			row.GrantType = grantType
		}
	}
}

// Detail is a finer label within the reason, such as org_lookup_failed. A value
// that is not a short identifier is left out and the row is kept.
func Detail(subReason string) Option {
	return func(row *Row) {
		if shortLabel.MatchString(subReason) {
			row.SubReason = subReason
		}
	}
}

// Retries is how many retries this step spent.
func Retries(n uint32) Option {
	return func(row *Row) { row.Retries = n }
}

// AuthClient is the mcp_auth_clients row.
func AuthClient(id uuid.UUID) Option {
	return func(row *Row) { row.AuthClientID = idString(id) }
}

// RemoteSessionIssuer is the upstream provider's remote_session_issuers row.
func RemoteSessionIssuer(id uuid.UUID) Option {
	return func(row *Row) { row.RemoteSessionIssuerID = idString(id) }
}

// UserSessionIssuer is the Speakeasy issuer's user_session_issuers row.
func UserSessionIssuer(id uuid.UUID) Option {
	return func(row *Row) { row.UserSessionIssuerID = idString(id) }
}

// UserSessionClient is the AI app's user_session_clients row.
func UserSessionClient(id uuid.UUID) Option {
	return func(row *Row) { row.UserSessionClientID = idString(id) }
}

// RemoteSessionClient is the remote_session_clients row.
func RemoteSessionClient(id uuid.UUID) Option {
	return func(row *Row) { row.RemoteSessionClientID = idString(id) }
}

// UserSession is the user_sessions row.
func UserSession(id uuid.UUID) Option {
	return func(row *Row) { row.UserSessionID = idString(id) }
}

// RemoteSession is the remote_sessions row.
func RemoteSession(id uuid.UUID) Option {
	return func(row *Row) { row.RemoteSessionID = idString(id) }
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
}

var errInvalidEvent = errors.New("invalid connection event")

// RowAt builds a row without recording it, for seed data. Flows use Step, End
// and Once. Outcome, result, reason and who fixes it come from what happened
// and the catalog, and outcome is only ever set on a final row.
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
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&row)
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
			row.TraceID, row.ProviderHost, row.AppKey,
		); err != nil {
			return fmt.Errorf("append connection event: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert connection events: %w", err)
	}
	return nil
}

// endedAttempt marks that an attempt's ending was written, across replicas.
type endedAttempt struct {
	ID string `json:"id"`
}

var _ cache.CacheableObject[endedAttempt] = (*endedAttempt)(nil)

func (e endedAttempt) CacheKey() string   { return "connectionAttemptEnded:" + e.ID }
func (e endedAttempt) TTL() time.Duration { return endedAttemptTTL }

// queuedRow is a row waiting for the writer.
type queuedRow struct {
	row Row

	// checkEnding is set for an ending on an attempt that can span requests.
	checkEnding bool
}

// FeatureChecker reads an organization's product features.
type FeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// config holds the write path's bounds.
type config struct {
	table               string
	queueSize           int
	maxBatch            int
	flushInterval       time.Duration
	insertTimeout       time.Duration
	featureCheckTimeout time.Duration
}

// Recorder writes connection events. Its methods only queue a row: one
// goroutine inserts, the queue is bounded, and rows that cannot be written are
// dropped and counted. It never retries, has no error for a caller to handle,
// and a nil Recorder does nothing. Rows of an organization with logs off are not
// written, which the writer checks so no call site has to.
type Recorder struct {
	logger   *slog.Logger
	conn     driver.Conn
	features FeatureChecker
	ended    cache.TypedCacheObject[endedAttempt]
	capture  func(Row)
	cfg      config
	steps    metric.Int64Counter
	dropped  metric.Int64Counter

	queue    chan queuedRow
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	stopped  atomic.Bool
	lastDrop atomic.Int64
}

// NewRecorder starts a recorder. Stop it with Shutdown. The cache enforces one
// ending per attempt across replicas. Without a feature checker nothing is written.
func NewRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, conn driver.Conn, cacheImpl cache.Cache, features FeatureChecker) *Recorder {
	return newRecorder(logger, meterProvider, conn, cacheImpl, features, config{
		table:               eventsTable,
		queueSize:           defaultQueueSize,
		maxBatch:            defaultMaxBatch,
		flushInterval:       defaultFlushInterval,
		insertTimeout:       defaultInsertTimeout,
		featureCheckTimeout: defaultFeatureCheckTimeout,
	})
}

func newRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, conn driver.Conn, cacheImpl cache.Cache, features FeatureChecker, cfg config) *Recorder {
	logger = logger.With(attr.SlogComponent("connection_events"))
	meter := meterProvider.Meter(meterScope)

	steps, err := meter.Int64Counter(meterSteps,
		metric.WithDescription("MCP connection attempt steps by outcome, step, reason, who fixes it and provider host."),
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
		logger:   logger,
		conn:     conn,
		features: features,
		ended:    cache.NewTypedObjectCache[endedAttempt](logger.With(attr.SlogCacheNamespace("connection_attempt_ended")), cacheImpl, cache.SuffixNone),
		capture:  nil,
		cfg:      cfg,
		steps:    steps,
		dropped:  dropped,
		queue:    make(chan queuedRow, cfg.queueSize),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		stopOnce: sync.Once{},
		stopped:  atomic.Bool{},
		lastDrop: atomic.Int64{},
	}
	go r.run()
	return r
}

// NewCapturing returns a recorder that hands each row to capture instead of
// writing it, for tests. Use connectioneventstest rather than calling this.
func NewCapturing(capture func(Row), features FeatureChecker) *Recorder {
	return &Recorder{
		logger:   slog.New(slog.DiscardHandler),
		conn:     nil,
		features: features,
		ended:    cache.TypedCacheObject[endedAttempt]{},
		capture:  capture,
		cfg:      config{table: "", queueSize: 0, maxBatch: 0, flushInterval: 0, insertTimeout: 0, featureCheckTimeout: time.Second},
		steps:    nil,
		dropped:  nil,
		queue:    nil,
		stop:     nil,
		done:     nil,
		stopOnce: sync.Once{},
		stopped:  atomic.Bool{},
		lastDrop: atomic.Int64{},
	}
}

// Step records a step that does not end the attempt.
func (r *Recorder) Step(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) {
	r.record(ctx, attempt, step, what, false, false, opts)
}

// End records the step that ends the attempt. Its outcome comes from what
// happened: Connected for a Result, and the catalog's outcome for a Code. Only
// the first ending of an attempt is written.
func (r *Recorder) End(ctx context.Context, attempt Attempt, step Step, what Happened, opts ...Option) {
	r.record(ctx, attempt, step, what, true, true, opts)
}

// Once records an attempt that is one request: one row, final.
func (r *Recorder) Once(ctx context.Context, scope Scope, step Step, what Happened, opts ...Option) {
	if r == nil {
		return
	}
	r.record(ctx, Begin(scope), step, what, true, false, opts)
}

func (r *Recorder) record(ctx context.Context, attempt Attempt, step Step, what Happened, final, checkEnding bool, opts []Option) {
	if r == nil {
		return
	}
	row, err := RowAt(attempt, step, what, final, time.Now(), opts...)
	if err != nil {
		r.drop(ctx, DropInvalidEvent, 1, err)
		return
	}
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.HasTraceID() {
		row.TraceID = spanCtx.TraceID().String()
	}
	if r.capture != nil {
		if r.checkOrganization(row.OrganizationID) == "" {
			r.capture(row)
		}
		return
	}

	if r.stopped.Load() {
		r.drop(ctx, DropShutdown, 1, nil)
		return
	}
	select {
	case r.queue <- queuedRow{row: row, checkEnding: checkEnding}:
	default:
		r.drop(ctx, DropQueueFull, 1, nil)
	}
}

// Shutdown writes what is queued, each batch once, and stops the goroutine.
func (r *Recorder) Shutdown(ctx context.Context) error {
	if r == nil || r.stop == nil {
		return nil
	}
	r.stopOnce.Do(func() {
		r.stopped.Store(true)
		close(r.stop)
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("drain connection events: %w", ctx.Err())
	}
}

func (r *Recorder) run() {
	defer close(r.done)

	ticker := time.NewTicker(r.cfg.flushInterval)
	defer ticker.Stop()

	// organizations holds each organization's logs check for one flush, so busy
	// traffic costs one lookup per interval and a setting change applies quickly.
	organizations := map[string]DropReason{}
	batch := make([]Row, 0, r.cfg.maxBatch)
	flush := func() {
		clear(organizations)
		if len(batch) > 0 {
			r.insert(batch)
			batch = batch[:0]
		}
	}
	add := func(queued queuedRow) {
		ctx := context.Background()
		reason, checked := organizations[queued.row.OrganizationID]
		if !checked {
			reason = r.checkOrganization(queued.row.OrganizationID)
			organizations[queued.row.OrganizationID] = reason
		}
		if reason != "" {
			r.drop(ctx, reason, 1, nil)
			return
		}
		if queued.checkEnding && r.alreadyEnded(queued.row) {
			return
		}
		r.countStep(ctx, queued.row)
		batch = append(batch, queued.row)
		if len(batch) >= r.cfg.maxBatch {
			flush()
		}
	}
	for {
		select {
		case queued := <-r.queue:
			add(queued)
		case <-ticker.C:
			flush()
		case <-r.stop:
			for {
				select {
				case queued := <-r.queue:
					add(queued)
				default:
					flush()
					return
				}
			}
		}
	}
}

// recordingEnabled decides whether an organization's attempts are stored. It
// follows the organization's logs setting, and is the only place that does.
func (r *Recorder) recordingEnabled(ctx context.Context, organizationID string) (bool, error) {
	if r.features == nil {
		return false, nil
	}
	enabled, err := r.features.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureLogs)
	if err != nil {
		return false, fmt.Errorf("check logs feature: %w", err)
	}
	return enabled, nil
}

// checkOrganization runs off the request path on its own context. It returns
// why the organization's rows are not written, or "" to write them. A check that
// fails or times out means do not write.
func (r *Recorder) checkOrganization(organizationID string) DropReason {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.featureCheckTimeout)
	defer cancel()

	enabled, err := r.recordingEnabled(ctx, organizationID)
	switch {
	case err != nil:
		r.logger.WarnContext(ctx, "check connection events allowed", attr.SlogError(err))
		return DropLogsCheck
	case !enabled:
		return DropLogsOff
	default:
		return ""
	}
}

// alreadyEnded claims the attempt's ending. A failed check lets the row through:
// the rollup counts an attempt once anyway, and losing an ending is worse.
func (r *Recorder) alreadyEnded(row Row) bool {
	ctx, cancel := context.WithTimeout(context.Background(), endingCheckTimeout)
	defer cancel()

	first, err := r.ended.StoreIfAbsent(ctx, endedAttempt{ID: row.AttemptID.String()})
	if err != nil {
		r.logger.WarnContext(ctx, "check connection attempt ending", attr.SlogError(err))
		return false
	}
	if !first {
		r.drop(ctx, DropDuplicateEnd, 1, nil)
	}
	return !first
}

// insert writes one batch once. async_insert=1 with no wait setting leaves the
// wait to the connection: production acks once ClickHouse has queued the rows.
func (r *Recorder) insert(rows []Row) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.insertTimeout)
	defer cancel()

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1}))
	err := appendRowsInto(ctx, r.conn, r.cfg.table, rows)
	if err == nil {
		return
	}
	reason := DropInsertFailed
	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok && exception.Code == unknownTableCode {
		reason = DropTableMissing
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
		attr.ConnectionProviderHost(row.ProviderHost),
	))
}

func (r *Recorder) drop(ctx context.Context, reason DropReason, rows int, cause error) {
	if r.dropped != nil {
		r.dropped.Add(ctx, int64(rows), metric.WithAttributes(attr.ConnectionDropReason(string(reason))))
	}
	if reason == DropLogsOff {
		// Expected for any organization with logs off, so it is only counted.
		return
	}
	now := time.Now().UnixNano()
	last := r.lastDrop.Load()
	if now-last < int64(dropLogInterval) || !r.lastDrop.CompareAndSwap(last, now) {
		return
	}
	attrs := []any{attr.SlogConnectionDropReason(string(reason))}
	if cause != nil {
		attrs = append(attrs, attr.SlogError(cause))
	}
	r.logger.WarnContext(ctx, "dropped connection events", attrs...)
}
