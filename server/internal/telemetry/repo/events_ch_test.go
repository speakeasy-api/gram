package repo_test

import (
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// The tests in this file exercise the org-scoped event feed queries against
// the otel_logs and otel_traces tables. Each test isolates itself with a
// random organization id, since the ClickHouse test container shares one
// database across tests.

type otelLogFixture struct {
	recordID           string
	orgID              string
	projectID          uuid.UUID
	timeUnixNano       int64
	source             string
	eventName          string
	body               string
	traceID            string
	spanID             string
	logAttributes      string
	resourceAttributes string
}

func newOtelLogFixture(orgID string, timeUnixNano int64) otelLogFixture {
	return otelLogFixture{
		recordID:           "log-" + uuid.NewString(),
		orgID:              orgID,
		projectID:          uuid.New(),
		timeUnixNano:       timeUnixNano,
		source:             "test-log-source",
		eventName:          "test.log.event",
		body:               "test log body",
		traceID:            "11111111111111111111111111111111",
		spanID:             "1111111111111111",
		logAttributes:      `{"test.attr":"log-value"}`,
		resourceAttributes: `{"service.name":"test-log-source"}`,
	}
}

func insertOtelLog(t *testing.T, conn clickhouse.Conn, row otelLogFixture) {
	t.Helper()

	err := conn.Exec(t.Context(), `
		INSERT INTO otel_logs (
			record_id, organization_id, project_id, time_unix_nano,
			source, event_name, body, trace_id, span_id,
			log_attributes, resource_attributes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, row.recordID, row.orgID, row.projectID, row.timeUnixNano,
		row.source, row.eventName, row.body, row.traceID, row.spanID,
		row.logAttributes, row.resourceAttributes)
	require.NoError(t, err)
}

type otelSpanFixture struct {
	recordID           string
	orgID              string
	projectID          uuid.UUID
	timeUnixNano       int64
	source             string
	spanName           string
	traceID            string
	spanID             string
	spanAttributes     string
	resourceAttributes string
}

func newOtelSpanFixture(orgID string, timeUnixNano int64) otelSpanFixture {
	return otelSpanFixture{
		recordID:           "span-" + uuid.NewString(),
		orgID:              orgID,
		projectID:          uuid.New(),
		timeUnixNano:       timeUnixNano,
		source:             "test-span-source",
		spanName:           "test.span.operation",
		traceID:            "22222222222222222222222222222222",
		spanID:             "2222222222222222",
		spanAttributes:     `{"test.attr":"span-value"}`,
		resourceAttributes: `{"service.name":"test-span-source"}`,
	}
}

func insertOtelSpan(t *testing.T, conn clickhouse.Conn, row otelSpanFixture) {
	t.Helper()

	err := conn.Exec(t.Context(), `
		INSERT INTO otel_traces (
			record_id, organization_id, project_id, time_unix_nano,
			source, span_name, trace_id, span_id,
			span_attributes, resource_attributes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, row.recordID, row.orgID, row.projectID, row.timeUnixNano,
		row.source, row.spanName, row.traceID, row.spanID,
		row.spanAttributes, row.resourceAttributes)
	require.NoError(t, err)
}

func eventTestWindow(base time.Time) (int64, int64) {
	return base.Add(-time.Hour).UnixNano(), base.Add(time.Hour).UnixNano()
}

func TestListEventLog_MergesLogsAndSpansNewestFirst(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	base := time.Now().UTC()

	oldest := newOtelLogFixture(orgID, base.Add(-3*time.Minute).UnixNano())
	middle := newOtelSpanFixture(orgID, base.Add(-2*time.Minute).UnixNano())
	newest := newOtelLogFixture(orgID, base.Add(-time.Minute).UnixNano())
	otherOrg := newOtelLogFixture("org-"+uuid.NewString(), base.Add(-time.Minute).UnixNano())

	insertOtelLog(t, conn, oldest)
	insertOtelSpan(t, conn, middle)
	insertOtelLog(t, conn, newest)
	insertOtelLog(t, conn, otherOrg)

	timeStart, timeEnd := eventTestWindow(base)
	items, err := queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters: repo.EventLogFilters{
			OrganizationID: orgID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Kinds:          nil,
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              10,
	})
	require.NoError(t, err)
	require.Len(t, items, 3)

	require.Equal(t, newest.recordID, items[0].RecordID)
	require.Equal(t, repo.EventKindLog, items[0].Kind)
	require.Equal(t, "test.log.event", items[0].Name)
	require.Equal(t, "test log body", items[0].BodyPreview)
	require.Equal(t, newest.projectID.String(), items[0].ProjectID)
	// The JSON column type interprets dotted keys as nested paths, so the
	// inserted {"test.attr": ...} reads back nested.
	require.JSONEq(t, `{"test":{"attr":"log-value"}}`, items[0].Attributes)
	require.JSONEq(t, `{"service":{"name":"test-log-source"}}`, items[0].ResourceAttributes)

	require.Equal(t, middle.recordID, items[1].RecordID)
	require.Equal(t, repo.EventKindSpan, items[1].Kind)
	require.Equal(t, "test.span.operation", items[1].Name)
	require.Empty(t, items[1].BodyPreview)
	require.JSONEq(t, `{"test":{"attr":"span-value"}}`, items[1].Attributes)

	require.Equal(t, oldest.recordID, items[2].RecordID)
}

func TestListEventLog_KeysetPaginationAcrossTables(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	base := time.Now().UTC()

	for i := range 2 {
		insertOtelLog(t, conn, newOtelLogFixture(orgID, base.Add(-time.Duration(2*i+1)*time.Minute).UnixNano()))
		insertOtelSpan(t, conn, newOtelSpanFixture(orgID, base.Add(-time.Duration(2*i+2)*time.Minute).UnixNano()))
	}

	timeStart, timeEnd := eventTestWindow(base)
	filters := repo.EventLogFilters{
		OrganizationID: orgID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		Kinds:          nil,
		Sources:        nil,
		Names:          nil,
		Search:         "",
	}

	firstPage, err := queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters:    filters,
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 2)

	secondPage, err := queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters:    filters,
		CursorTimeUnixNano: firstPage[1].TimeUnixNano,
		CursorRecordID:     firstPage[1].RecordID,
		Limit:              2,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 2)

	seen := map[string]bool{}
	var allTimes []int64
	for _, item := range append(firstPage, secondPage...) {
		require.False(t, seen[item.RecordID], "record %s returned twice", item.RecordID)
		seen[item.RecordID] = true
		allTimes = append(allTimes, item.TimeUnixNano)
	}
	for i := 1; i < len(allTimes); i++ {
		require.Greater(t, allTimes[i-1], allTimes[i], "pages must be globally ordered newest first")
	}
}

func TestListEventLog_TruncatesBodyPreview(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	base := time.Now().UTC()

	row := newOtelLogFixture(orgID, base.UnixNano())
	longBody := make([]byte, 0, 500)
	for range 500 {
		longBody = append(longBody, 'a')
	}
	row.body = string(longBody)
	insertOtelLog(t, conn, row)

	timeStart, timeEnd := eventTestWindow(base)
	items, err := queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters: repo.EventLogFilters{
			OrganizationID: orgID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Kinds:          nil,
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              1,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].BodyPreview, repo.EventBodyPreviewChars)
}

func TestListEventLog_KindSourceAndSearchFilters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	base := time.Now().UTC()

	log := newOtelLogFixture(orgID, base.Add(-time.Minute).UnixNano())
	log.body = "the Needle is in the body"
	span := newOtelSpanFixture(orgID, base.Add(-2*time.Minute).UnixNano())
	span.spanName = "operation.with.needle"
	insertOtelLog(t, conn, log)
	insertOtelSpan(t, conn, span)

	timeStart, timeEnd := eventTestWindow(base)
	baseFilters := repo.EventLogFilters{
		OrganizationID: orgID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		Kinds:          nil,
		Sources:        nil,
		Names:          nil,
		Search:         "",
	}

	kindFilters := baseFilters
	kindFilters.Kinds = []string{repo.EventKindSpan}
	items, err := queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters:    kindFilters,
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              10,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, span.recordID, items[0].RecordID)

	sourceFilters := baseFilters
	sourceFilters.Sources = []string{"test-log-source"}
	items, err = queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters:    sourceFilters,
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              10,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, log.recordID, items[0].RecordID)

	nameFilters := baseFilters
	nameFilters.Names = []string{"operation.with.needle"}
	items, err = queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters:    nameFilters,
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              10,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, span.recordID, items[0].RecordID)

	// Case-insensitive search matches the log body and the span name.
	searchFilters := baseFilters
	searchFilters.Search = "needle"
	items, err = queries.ListEventLog(ctx, repo.ListEventLogParams{
		EventLogFilters:    searchFilters,
		CursorTimeUnixNano: 0,
		CursorRecordID:     "",
		Limit:              10,
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestCountEventLog_CountsAcrossTables(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	base := time.Now().UTC()

	insertOtelLog(t, conn, newOtelLogFixture(orgID, base.Add(-time.Minute).UnixNano()))
	insertOtelLog(t, conn, newOtelLogFixture(orgID, base.Add(-2*time.Minute).UnixNano()))
	insertOtelSpan(t, conn, newOtelSpanFixture(orgID, base.Add(-3*time.Minute).UnixNano()))

	timeStart, timeEnd := eventTestWindow(base)
	total, capped, err := queries.CountEventLog(ctx, repo.EventLogFilters{
		OrganizationID: orgID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		Kinds:          nil,
		Sources:        nil,
		Names:          nil,
		Search:         "",
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.False(t, capped)
}

func TestGetEventVolume_BucketsLogsAndSpans(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	// Aligned to a minute boundary so bucket math is exact.
	base := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)

	insertOtelLog(t, conn, newOtelLogFixture(orgID, base.Add(5*time.Second).UnixNano()))
	insertOtelLog(t, conn, newOtelLogFixture(orgID, base.Add(10*time.Second).UnixNano()))
	insertOtelSpan(t, conn, newOtelSpanFixture(orgID, base.Add(15*time.Second).UnixNano()))
	insertOtelSpan(t, conn, newOtelSpanFixture(orgID, base.Add(time.Minute+5*time.Second).UnixNano()))

	rows, err := queries.GetEventVolume(ctx, repo.GetEventVolumeParams{
		EventLogFilters: repo.EventLogFilters{
			OrganizationID: orgID,
			TimeStart:      base.UnixNano(),
			TimeEnd:        base.Add(2 * time.Minute).UnixNano(),
			Kinds:          nil,
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		IntervalSeconds: 60,
	})
	require.NoError(t, err)

	firstBucket := base.UnixNano()
	secondBucket := base.Add(time.Minute).UnixNano()
	counts := map[int64]map[string]uint64{}
	for _, row := range rows {
		if counts[row.BucketUnixNano] == nil {
			counts[row.BucketUnixNano] = map[string]uint64{}
		}
		counts[row.BucketUnixNano][row.Kind] = row.EventCount
	}

	require.Equal(t, uint64(2), counts[firstBucket][repo.EventKindLog])
	require.Equal(t, uint64(1), counts[firstBucket][repo.EventKindSpan])
	require.Equal(t, uint64(1), counts[secondBucket][repo.EventKindSpan])
	require.Zero(t, counts[secondBucket][repo.EventKindLog])
}

func TestGetEventFacets_DistinctSourcesAndNames(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	orgID := "org-" + uuid.NewString()
	base := time.Now().UTC()

	// A source shared by a log and a span must appear once.
	log1 := newOtelLogFixture(orgID, base.Add(-time.Minute).UnixNano())
	log1.source = "shared-source"
	log2 := newOtelLogFixture(orgID, base.Add(-2*time.Minute).UnixNano())
	log2.source = "shared-source"
	log2.eventName = "another.log.event"
	span1 := newOtelSpanFixture(orgID, base.Add(-3*time.Minute).UnixNano())
	span1.source = "shared-source"
	span2 := newOtelSpanFixture(orgID, base.Add(-4*time.Minute).UnixNano())

	insertOtelLog(t, conn, log1)
	insertOtelLog(t, conn, log2)
	insertOtelSpan(t, conn, span1)
	insertOtelSpan(t, conn, span2)

	timeStart, timeEnd := eventTestWindow(base)
	facets, err := queries.GetEventFacets(ctx, repo.EventLogFilters{
		OrganizationID: orgID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		Kinds:          nil,
		Sources:        nil,
		Names:          nil,
		Search:         "",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"shared-source", "test-span-source"}, facets.Sources)
	require.Equal(t, []string{"another.log.event", "test.log.event", "test.span.operation"}, facets.Names)
}
