package o11y

import (
	"context"
	"log/slog"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// meterClickhouseQueryDuration measures every ClickHouse client call routed
// through a traced connection (Query/Select/Exec), labeled with the
// low-cardinality target table and outcome — the read-side counterpart of
// telemetry.clickhouse.write.duration, and the series to build per-table
// latency dashboards and monitors on (INC-417).
const meterClickhouseQueryDuration = "clickhouse.client.query.duration"

// tracedClickhouseConn decorates a clickhouse.Conn so every query issued
// through it — by any repo, current or future — emits a client span plus a
// duration metric, labeled with the issuing function
// (gram.clickhouse.operation) and target table. The full SQL text is
// deliberately NOT attached: the operation name identifies the code that
// builds the query, and omitting the text keeps span ingest volume flat.
// The span context is forwarded to ClickHouse so the server-side execution
// spans (system.opentelemetry_span_log) join the request trace. clickhouse-go only
// sends trace context that is explicitly attached via
// clickhouse.Context/WithSpan; it never reads the span from ctx itself.
//
// PrepareBatch and AsyncInsert pass through untraced (no batch caller runs
// inside the server processes today; wrap driver.Batch here before adopting
// one on a hot path). The synchronous telemetry writes go through Exec and
// are therefore traced here at the connection layer AND measured at the
// Logger layer (observeCHWrite): the layers are complementary, not
// duplicates — telemetry.clickhouse.write.duration is the logger-level
// operation (stable snake_case names, row counts, retry-inclusive), while
// clickhouse.client.query.duration is the per-call connection layer. Do not
// sum the two metrics.
type tracedClickhouseConn struct {
	clickhouse.Conn
	tracer        trace.Tracer
	queryDuration metric.Float64Histogram
}

// TraceClickhouseConn wraps conn so all Query, Select, and Exec calls are
// traced and measured by default. Wrap once at connection creation
// (cmd/gram's newClickhouseClient); everything built on the connection
// inherits the instrumentation without per-call-site wiring.
func TraceClickhouseConn(conn clickhouse.Conn, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) clickhouse.Conn {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/o11y")
	queryDuration, err := meter.Float64Histogram(
		meterClickhouseQueryDuration,
		metric.WithDescription("Duration of ClickHouse client calls in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterClickhouseQueryDuration), attr.SlogError(err))
	}

	return &tracedClickhouseConn{
		Conn:          conn,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/o11y"),
		queryDuration: queryDuration,
	}
}

// clickhouseTablePatterns extract the primary table a statement targets, for
// the metric/span label only — a mismatch can mislabel a dashboard bucket but
// never affects query behavior, and the operation label names the function
// whose SQL is the ground truth. Inputs are exclusively our own placeholder-parameterized SQL
// (user values are bound as args, never interpolated). Known label limits,
// pinned by TestClickhouseTargetTable: the first FROM wins (a CTE reading
// another table labels that table; JOINs label the leading table), and an
// unmatched statement labels "unknown". The FROM pattern skips subquery
// parens by requiring an identifier, so a
// "FROM (SELECT ... FROM chat_session_summaries s)" resolves to the inner
// table.
// A single alternation keeps this to ONE scan of the SQL text (three
// sequential patterns cost ~200us on our largest ~8KB queries; one early-
// matching scan is near-free). First match by position preserves the
// intended precedence: INSERT INTO / ALTER TABLE lead their statements, and
// for SELECTs the first FROM wins. An optional database qualifier
// (FROM db.table) is skipped so the TABLE segment is captured.
var clickhouseTablePattern = regexp.MustCompile(
	`(?i)\b(?:INSERT\s+INTO|ALTER\s+TABLE|FROM)\s+` + "`?" + `(?:[a-zA-Z_][a-zA-Z0-9_]*\.)?` + "`?" + `([a-zA-Z_][a-zA-Z0-9_]*)`)

// receiverCleaner strips Go method-receiver punctuation from stack-frame
// names, e.g. (*Queries).ListSessions -> Queries.ListSessions. Built once:
// strings.Replacer compiles an internal machine per instance.
var receiverCleaner = strings.NewReplacer("(", "", ")", "", "*", "")

func clickhouseTargetTable(query string) string {
	if m := clickhouseTablePattern.FindStringSubmatch(query); m != nil {
		return m[1]
	}
	return "unknown"
}

// clickhouseCallerOperation names the query after the function that issued
// it: the first stack frame outside this package (and the runtime), which is
// the repo method or caller function — e.g. ListSessions or
// listSessionsFromSummaries. Automatic for every query with no per-call-site
// wiring, and bounded in cardinality by the number of query functions.
func clickhouseCallerOperation() string {
	pc := make([]uintptr, 16)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		// Skip this decorator's own frames by name rather than frame count:
		// counting is fragile under inlining.
		if frame.Function != "" &&
			!strings.Contains(frame.Function, "o11y.clickhouseCallerOperation") &&
			!strings.Contains(frame.Function, "o11y.(*tracedClickhouseConn)") {
			name := frame.Function
			if i := strings.LastIndex(name, "/"); i >= 0 {
				name = name[i+1:]
			}
			// Trim the package qualifier, keeping Type.Method or function.
			if i := strings.Index(name, "."); i >= 0 {
				name = name[i+1:]
			}
			return receiverCleaner.Replace(name)
		}
		if !more {
			return "unknown"
		}
	}
}

// start opens the client span and returns the ClickHouse-bound context
// (span context attached for server-side spans) plus a done callback that
// records status + the duration metric and ends the span. done is
// idempotent: only its first invocation records.
func (c *tracedClickhouseConn) start(ctx context.Context, spanName, query string) (context.Context, func(err error)) {
	table := clickhouseTargetTable(query)
	operation := clickhouseCallerOperation()
	ctx, span := c.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemNameClickHouse,
			// Legacy key alongside db.system.name: Datadog's database
			// facets key on db.system, and deps.go's Redis spans still
			// emit the pre-rename semconv. Drop once the binary
			// standardizes on one semconv version.
			attribute.String("db.system", "clickhouse"),
			attr.ClickhouseTable(table),
			attr.ClickhouseOperation(operation),
		),
	)
	if sc := span.SpanContext(); sc.IsValid() {
		// clickhouse-go v2 merges: Context() seeds from the parent's
		// existing QueryOptions before applying WithSpan, so caller options
		// (WithAsync, settings, parameters) survive this wrap. That merge
		// is driver behavior, not contract — re-verify on driver upgrades.
		ctx = clickhouse.Context(ctx, clickhouse.WithSpan(sc))
	}

	begin := time.Now()
	completed := false
	done := func(err error) {
		if completed {
			return
		}
		completed = true
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if c.queryDuration != nil {
			c.queryDuration.Record(ctx, time.Since(begin).Seconds(), metric.WithAttributes(
				attr.ClickhouseTable(table),
				attr.ClickhouseOperation(operation),
				attr.Outcome(OutcomeFromErrorWithTimeout(err)),
			))
		}
		span.End()
	}
	return ctx, done
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Exec(ctx context.Context, query string, args ...any) error {
	ctx, done := c.start(ctx, "clickhouse.exec", query)
	err := c.Conn.Exec(ctx, query, args...)
	done(err)
	return err
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	ctx, done := c.start(ctx, "clickhouse.select", query)
	err := c.Conn.Select(ctx, dest, query, args...)
	done(err)
	return err
}

func (c *tracedClickhouseConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	ctx, done := c.start(ctx, "clickhouse.query_row", query)
	row := c.Conn.QueryRow(ctx, query, args...)
	// driver.Row is lazy: the terminal call (Scan/ScanStruct/Err) completes
	// the span and metric.
	return &tracedClickhouseRow{Row: row, done: done}
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	ctx, done := c.start(ctx, "clickhouse.query", query)
	rows, err := c.Conn.Query(ctx, query, args...)
	if err != nil {
		done(err)
		return nil, err
	}
	// ClickHouse streams results: finishing here would measure only dispatch,
	// not the query's real duration. The span and metric complete when the
	// caller closes the result set.
	return &tracedClickhouseRows{Rows: rows, done: done}, nil
}

// tracedClickhouseRows completes the query span and duration metric when the
// result set ends: at stream exhaustion (Next returning false — clickhouse-go
// releases the stream there, and callers may legally skip Close after it) or
// at Close, whichever comes first. done is idempotent, so hitting both — or a
// double Close — records exactly once.
type tracedClickhouseRows struct {
	driver.Rows
	done func(err error)
}

func (r *tracedClickhouseRows) Next() bool {
	next := r.Rows.Next()
	if !next {
		r.done(r.Err())
	}
	return next
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (r *tracedClickhouseRows) Close() error {
	err := r.Rows.Close()
	r.done(err)
	return err
}

// tracedClickhouseRow completes the query_row span and duration metric on
// the first terminal call. done is idempotent.
type tracedClickhouseRow struct {
	driver.Row
	done func(err error)
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (r *tracedClickhouseRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.done(err)
	return err
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (r *tracedClickhouseRow) ScanStruct(dest any) error {
	err := r.Row.ScanStruct(dest)
	r.done(err)
	return err
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (r *tracedClickhouseRow) Err() error {
	err := r.Row.Err()
	r.done(err)
	return err
}
