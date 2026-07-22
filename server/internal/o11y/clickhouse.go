package o11y

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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
// through it — by any repo, current or future — emits a client span carrying
// the query text (db.query.text) plus a duration metric, with the span
// context forwarded to ClickHouse so the server-side execution spans
// (system.opentelemetry_span_log) join the request trace. clickhouse-go only
// sends trace context that is explicitly attached via
// clickhouse.Context/WithSpan; it never reads the span from ctx itself.
//
// PrepareBatch and AsyncInsert pass through untraced: the synchronous
// telemetry write path is instrumented at the Logger layer (observeCHWrite),
// which owns its own spans and metric.
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
// never affects query behavior, and the full query text rides the span as
// ground truth. Inputs are exclusively our own placeholder-parameterized SQL
// (user values are bound as args, never interpolated). Known label limits,
// pinned by TestClickhouseTargetTable: the first FROM wins (a CTE reading
// another table labels that table; JOINs label the leading table), and an
// unmatched statement labels "unknown". The FROM pattern skips subquery
// parens by requiring an identifier, so a
// "FROM (SELECT ... FROM chat_session_summaries s)" resolves to the inner
// table.
var clickhouseTablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+` + "`?" + `([a-zA-Z_][a-zA-Z0-9_]*)`),
	regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+` + "`?" + `([a-zA-Z_][a-zA-Z0-9_]*)`),
	regexp.MustCompile(`(?i)\bFROM\s+` + "`?" + `([a-zA-Z_][a-zA-Z0-9_]*)`),
}

func clickhouseTargetTable(query string) string {
	for _, pattern := range clickhouseTablePatterns {
		if m := pattern.FindStringSubmatch(query); m != nil {
			return m[1]
		}
	}
	return "unknown"
}

// start opens the client span and returns everything the call needs to
// finish it: the ClickHouse-bound context (span context attached for
// server-side spans), the span, and a done callback recording status +
// duration metric.
func (c *tracedClickhouseConn) start(ctx context.Context, spanName, query string) (context.Context, trace.Span, func(err error)) {
	table := clickhouseTargetTable(query)
	ctx, span := c.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.DBSystemNameClickHouse, semconv.DBQueryText(query), attr.ClickhouseTable(table)),
	)
	if sc := span.SpanContext(); sc.IsValid() {
		ctx = clickhouse.Context(ctx, clickhouse.WithSpan(sc))
	}

	begin := time.Now()
	done := func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if c.queryDuration != nil {
			c.queryDuration.Record(ctx, time.Since(begin).Seconds(), metric.WithAttributes(
				attr.ClickhouseTable(table),
				attr.Outcome(OutcomeFromErrorWithTimeout(err)),
			))
		}
		span.End()
	}
	return ctx, span, done
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Exec(ctx context.Context, query string, args ...any) error {
	ctx, _, done := c.start(ctx, "clickhouse.exec", query)
	err := c.Conn.Exec(ctx, query, args...)
	done(err)
	return err
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	ctx, _, done := c.start(ctx, "clickhouse.select", query)
	err := c.Conn.Select(ctx, dest, query, args...)
	done(err)
	return err
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	ctx, _, done := c.start(ctx, "clickhouse.query", query)
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
// result set is closed.
type tracedClickhouseRows struct {
	driver.Rows
	done func(err error)
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (r *tracedClickhouseRows) Close() error {
	err := r.Rows.Close()
	r.done(err)
	return err
}
