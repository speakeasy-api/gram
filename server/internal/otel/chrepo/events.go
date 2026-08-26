package chrepo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
)

// Event kinds served by the org-scoped event feed. The vocabulary is closed
// and server-side: each kind maps to one signal table.
const (
	EventKindLog  = "log"
	EventKindSpan = "span"
)

// EventBodyPreviewChars is how many characters of a log body listEventLog
// returns. The full body stays in ClickHouse; the preview bounds payload size.
const EventBodyPreviewChars = 200

// EventLogCountCap bounds the total_count reported by CountEventLog. Counting
// stops once this many matching rows have been seen, so the "n of m events"
// display stays cheap on large orgs.
const EventLogCountCap = 10000

// eventFacetValueLimit bounds how many distinct values each per-table facet
// subquery contributes, keeping the facets payload small even for orgs with
// pathological name cardinality.
const eventFacetValueLimit = 1000

// eventTableSpec describes how one signal table maps onto the merged event
// feed shape.
type eventTableSpec struct {
	// table is the ClickHouse table holding this signal.
	table string

	// kind is the synthetic kind literal rows from this table carry.
	kind string

	// nameCol is the column exposed as the event name.
	nameCol string

	// bodyCol is the raw body column; empty for signals without a body.
	bodyCol string

	// attrsCol is the JSON column holding the signal's own attributes.
	attrsCol string
}

var eventTables = []eventTableSpec{
	{table: "otel_logs", kind: EventKindLog, nameCol: "event_name", bodyCol: "body", attrsCol: "log_attributes"},
	{table: "otel_traces", kind: EventKindSpan, nameCol: "span_name", bodyCol: "", attrsCol: "span_attributes"},
}

// selectedEventTables returns the signal tables a kind filter keeps. An empty
// filter selects every table; unknown kinds simply match nothing.
func selectedEventTables(kinds []string) []eventTableSpec {
	if len(kinds) == 0 {
		return eventTables
	}
	out := make([]eventTableSpec, 0, len(eventTables))
	for _, spec := range eventTables {
		if slices.Contains(kinds, spec.kind) {
			out = append(out, spec)
		}
	}
	return out
}

// EventLogFilters is the filter set shared by the event feed queries. All
// fields except OrganizationID and the time window are optional.
type EventLogFilters struct {
	OrganizationID string
	TimeStart      int64
	TimeEnd        int64

	// Kinds restricts which signal tables are read (log/span). Empty means both.
	Kinds []string

	// Sources restricts to these canonicalized sources (ORed).
	Sources []string

	// Names restricts to these event/span names (ORed).
	Names []string

	// Search is a case-insensitive substring match over body and name.
	Search string
}

// applyEventFilters attaches the shared filter predicates for one signal table.
func (spec eventTableSpec) applyEventFilters(sb squirrel.SelectBuilder, f EventLogFilters) squirrel.SelectBuilder {
	sb = sb.
		Where("organization_id = ?", f.OrganizationID).
		Where("time_unix_nano >= ?", f.TimeStart).
		Where("time_unix_nano <= ?", f.TimeEnd)

	if len(f.Sources) > 0 {
		sb = sb.Where(squirrel.Eq{"source": f.Sources})
	}
	if len(f.Names) > 0 {
		sb = sb.Where(squirrel.Eq{spec.nameCol: f.Names})
	}
	if f.Search != "" {
		if spec.bodyCol != "" {
			sb = sb.Where(
				"(positionCaseInsensitiveUTF8("+spec.bodyCol+", ?) > 0 OR positionCaseInsensitiveUTF8("+spec.nameCol+", ?) > 0)",
				f.Search, f.Search,
			)
		} else {
			sb = sb.Where("positionCaseInsensitiveUTF8("+spec.nameCol+", ?) > 0", f.Search)
		}
	}

	return sb
}

// eventUnionSource renders per-table subqueries as a parenthesized UNION ALL
// expression usable as a squirrel FROM source, with the flattened argument
// list in subquery order.
func eventUnionSource(subs []squirrel.SelectBuilder) (string, []any, error) {
	parts := make([]string, 0, len(subs))
	args := make([]any, 0)
	for _, sb := range subs {
		sql, subArgs, err := sb.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("build event subquery: %w", err)
		}
		parts = append(parts, "("+sql+")")
		args = append(args, subArgs...)
	}
	return "(" + strings.Join(parts, " UNION ALL ") + ")", args, nil
}

// ListEventLogParams selects one page of the merged event feed.
type ListEventLogParams struct {
	EventLogFilters

	// CursorTimeUnixNano holds the keyset position (event time of the last
	// event on the previous page); zero means first page. The next page
	// resumes at strictly older times, so events sharing the boundary
	// nanosecond are skipped — an accepted trade-off at nanosecond
	// resolution.
	CursorTimeUnixNano int64

	Limit int
}

// EventLogRow is one merged feed event as returned by ClickHouse.
type EventLogRow struct {
	TimeUnixNano       int64  `ch:"time_unix_nano"`
	Kind               string `ch:"kind"`
	Source             string `ch:"source"`
	Name               string `ch:"name"`
	BodyPreview        string `ch:"body_preview"`
	TraceID            string `ch:"trace_id"`
	SpanID             string `ch:"span_id"`
	ProjectID          string `ch:"project_id"`
	Attributes         string `ch:"attributes"`
	ResourceAttributes string `ch:"resource_attributes"`
}

var eventLogOuterColumns = []string{
	"time_unix_nano",
	"kind",
	"source",
	"name",
	"body_preview",
	"trace_id",
	"span_id",
	"project_id",
	"attributes",
	"resource_attributes",
}

// buildListEventLogQuery assembles the merged feed page read: one aligned
// subquery per selected signal table (own filters, keyset predicate, inner
// LIMIT), UNION ALL, then an outer sort and LIMIT over the merged rows.
func buildListEventLogQuery(arg ListEventLogParams) (string, []any, error) {
	subs := make([]squirrel.SelectBuilder, 0, len(eventTables))
	for _, spec := range selectedEventTables(arg.Kinds) {
		bodyPreview := "'' AS body_preview"
		if spec.bodyCol != "" {
			bodyPreview = fmt.Sprintf("leftUTF8(%s, %d) AS body_preview", spec.bodyCol, EventBodyPreviewChars)
		}
		sb := sq.Select(
			"time_unix_nano",
			"'"+spec.kind+"' AS kind",
			"source",
			spec.nameCol+" AS name",
			bodyPreview,
			"trace_id",
			"span_id",
			"toString(project_id) AS project_id",
			"toString("+spec.attrsCol+") AS attributes",
			"toString(resource_attributes) AS resource_attributes",
		).From(spec.table)
		sb = spec.applyEventFilters(sb, arg.EventLogFilters)
		if arg.CursorTimeUnixNano != 0 {
			sb = sb.Where("time_unix_nano < ?", arg.CursorTimeUnixNano)
		}
		sb = sb.
			OrderBy("time_unix_nano DESC").
			Suffix(fmt.Sprintf("LIMIT %d", arg.Limit))
		subs = append(subs, sb)
	}

	source, sourceArgs, err := eventUnionSource(subs)
	if err != nil {
		return "", nil, err
	}

	outer := sq.Select(eventLogOuterColumns...).
		From(source).
		OrderBy("time_unix_nano DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // callers pass a validated positive limit

	query, args, err := outer.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build event log query: %w", err)
	}
	// squirrel places the FROM subquery's placeholders after the outer
	// builder's, and this outer builder contributes none, so the source
	// arguments lead.
	return query, append(sourceArgs, args...), nil
}

// ListEventLog returns one page of the org's merged event feed, newest first
// with time_unix_nano keyset pagination. The tables are plain MergeTree fed
// at-least-once, so a Pub/Sub redelivery can surface the same event twice;
// readers tolerate duplicates like standard OTel ClickHouse tables do.
func (q *Queries) ListEventLog(ctx context.Context, arg ListEventLogParams) ([]EventLogRow, error) {
	if arg.Limit <= 0 || len(selectedEventTables(arg.Kinds)) == 0 {
		return []EventLogRow{}, nil
	}

	query, args, err := buildListEventLogQuery(arg)
	if err != nil {
		return nil, err
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query event log: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]EventLogRow, 0)
	for rows.Next() {
		var row EventLogRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan event log row: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event log rows: %w", err)
	}
	return items, nil
}

// buildCountEventLogQuery assembles the capped count: each signal table
// contributes at most EventLogCountCap+1 matching rows, so counting stops
// scanning shortly past the cap instead of aggregating the full range.
func buildCountEventLogQuery(arg EventLogFilters) (string, []any, error) {
	subs := make([]squirrel.SelectBuilder, 0, len(eventTables))
	for _, spec := range selectedEventTables(arg.Kinds) {
		sb := sq.Select("1 AS one").From(spec.table)
		sb = spec.applyEventFilters(sb, arg)
		sb = sb.Limit(uint64(EventLogCountCap) + 1)
		subs = append(subs, sb)
	}

	source, sourceArgs, err := eventUnionSource(subs)
	if err != nil {
		return "", nil, err
	}

	query, args, err := sq.Select("count() AS total").From(source).ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build event log count query: %w", err)
	}
	return query, append(sourceArgs, args...), nil
}

// CountEventLog returns how many events match the filters, capped at
// EventLogCountCap, and whether the cap was hit.
func (q *Queries) CountEventLog(ctx context.Context, arg EventLogFilters) (int64, bool, error) {
	if len(selectedEventTables(arg.Kinds)) == 0 {
		return 0, false, nil
	}

	query, args, err := buildCountEventLogQuery(arg)
	if err != nil {
		return 0, false, err
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, false, fmt.Errorf("query event log count: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var total uint64
	if rows.Next() {
		if err := rows.Scan(&total); err != nil {
			return 0, false, fmt.Errorf("scan event log count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("iterate event log count rows: %w", err)
	}

	if total > EventLogCountCap {
		return EventLogCountCap, true, nil
	}
	return int64(total), false, nil
}

// GetEventVolumeParams selects the bucketed log/span counts for a range.
type GetEventVolumeParams struct {
	EventLogFilters

	IntervalSeconds int64
}

// EventVolumeRow is one (bucket, kind) count. Buckets with no events are
// absent; callers zero-fill.
type EventVolumeRow struct {
	BucketUnixNano int64  `ch:"bucket_ns"`
	Kind           string `ch:"kind"`
	EventCount     uint64 `ch:"event_count"`
}

// buildEventVolumeQuery assembles the per-kind bucketed counts: each signal
// table groups its own rows by toStartOfInterval bucket, and the UNION ALL
// needs no outer re-aggregation because the tables carry disjoint kinds.
func buildEventVolumeQuery(arg GetEventVolumeParams) (string, []any, error) {
	subs := make([]squirrel.SelectBuilder, 0, len(eventTables))
	for _, spec := range selectedEventTables(arg.Kinds) {
		sb := sq.Select().
			Column(squirrel.Expr(
				"toInt64(toStartOfInterval(fromUnixTimestamp64Nano(time_unix_nano), toIntervalSecond(?))) * 1000000000 AS bucket_ns",
				arg.IntervalSeconds,
			)).
			Column("'" + spec.kind + "' AS kind").
			Column("count() AS event_count").
			From(spec.table)
		sb = spec.applyEventFilters(sb, arg.EventLogFilters)
		sb = sb.GroupBy("bucket_ns")
		subs = append(subs, sb)
	}

	source, sourceArgs, err := eventUnionSource(subs)
	if err != nil {
		return "", nil, err
	}

	query, args, err := sq.Select("bucket_ns", "kind", "event_count").
		From(source).
		OrderBy("bucket_ns ASC", "kind ASC").
		ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build event volume query: %w", err)
	}
	return query, append(sourceArgs, args...), nil
}

// GetEventVolume returns bucketed log/span counts for the range.
func (q *Queries) GetEventVolume(ctx context.Context, arg GetEventVolumeParams) ([]EventVolumeRow, error) {
	if arg.IntervalSeconds <= 0 {
		return nil, fmt.Errorf("event volume interval must be positive, got %d", arg.IntervalSeconds)
	}
	if len(selectedEventTables(arg.Kinds)) == 0 {
		return []EventVolumeRow{}, nil
	}

	query, args, err := buildEventVolumeQuery(arg)
	if err != nil {
		return nil, err
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query event volume: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]EventVolumeRow, 0)
	for rows.Next() {
		var row EventVolumeRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan event volume row: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event volume rows: %w", err)
	}
	return items, nil
}

// EventFacets holds the distinct filterable values observed in a range.
type EventFacets struct {
	Sources []string
	Names   []string
}

const (
	eventFacetSource = "source"
	eventFacetName   = "name"
)

// buildEventFacetsQuery assembles the facet read: every selected signal table
// contributes its distinct sources and names (empty values excluded, capped
// per subquery), and the outer DISTINCT folds values shared across tables.
func buildEventFacetsQuery(arg EventLogFilters) (string, []any, error) {
	subs := make([]squirrel.SelectBuilder, 0, 2*len(eventTables))
	for _, spec := range selectedEventTables(arg.Kinds) {
		src := sq.Select("'"+eventFacetSource+"' AS facet", "source AS value").From(spec.table)
		src = spec.applyEventFilters(src, arg)
		src = src.Where("source != ''").
			GroupBy("source").
			OrderBy("value ASC").
			Limit(eventFacetValueLimit)
		subs = append(subs, src)

		nm := sq.Select("'"+eventFacetName+"' AS facet", spec.nameCol+" AS value").From(spec.table)
		nm = spec.applyEventFilters(nm, arg)
		nm = nm.Where(spec.nameCol + " != ''").
			GroupBy(spec.nameCol).
			OrderBy("value ASC").
			Limit(eventFacetValueLimit)
		subs = append(subs, nm)
	}

	source, sourceArgs, err := eventUnionSource(subs)
	if err != nil {
		return "", nil, err
	}

	query, args, err := sq.Select("facet", "value").
		Distinct().
		From(source).
		OrderBy("facet ASC", "value ASC").
		ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build event facets query: %w", err)
	}
	return query, append(sourceArgs, args...), nil
}

// GetEventFacets returns the distinct sources and event/span names observed
// in the range, each list ascending.
func (q *Queries) GetEventFacets(ctx context.Context, arg EventLogFilters) (EventFacets, error) {
	facets := EventFacets{Sources: []string{}, Names: []string{}}
	if len(selectedEventTables(arg.Kinds)) == 0 {
		return facets, nil
	}

	query, args, err := buildEventFacetsQuery(arg)
	if err != nil {
		return facets, err
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return facets, fmt.Errorf("query event facets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var facet, value string
		if err := rows.Scan(&facet, &value); err != nil {
			return facets, fmt.Errorf("scan event facet row: %w", err)
		}
		switch facet {
		case eventFacetSource:
			facets.Sources = append(facets.Sources, value)
		case eventFacetName:
			facets.Names = append(facets.Names, value)
		}
	}
	if err := rows.Err(); err != nil {
		return facets, fmt.Errorf("iterate event facet rows: %w", err)
	}
	return facets, nil
}
