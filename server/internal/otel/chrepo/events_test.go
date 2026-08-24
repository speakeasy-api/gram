package chrepo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func fullEventLogFilters() EventLogFilters {
	return EventLogFilters{
		OrganizationID: "org-under-test",
		TimeStart:      1,
		TimeEnd:        2,
		Kinds:          nil,
		Sources:        []string{"claude-code"},
		Names:          []string{"gen_ai.client.inference"},
		Search:         "needle",
	}
}

func TestBuildListEventLogQuery_PlaceholdersMatchArgs(t *testing.T) {
	t.Parallel()

	sql, args, err := buildListEventLogQuery(ListEventLogParams{
		EventLogFilters:    fullEventLogFilters(),
		CursorTimeUnixNano: 42,
		Limit:              51,
	})
	require.NoError(t, err)
	require.Len(t, args, strings.Count(sql, "?"), "sql: %s\nargs: %#v", sql, args)
}

func TestBuildListEventLogQuery_MergesBothTables(t *testing.T) {
	t.Parallel()

	sql, _, err := buildListEventLogQuery(ListEventLogParams{
		EventLogFilters: EventLogFilters{
			OrganizationID: "org-under-test",
			TimeStart:      1,
			TimeEnd:        2,
			Kinds:          nil,
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		CursorTimeUnixNano: 0,
		Limit:              10,
	})
	require.NoError(t, err)
	require.Contains(t, sql, "otel_logs")
	require.Contains(t, sql, "otel_traces")
	require.Contains(t, sql, "UNION ALL")
	require.Contains(t, sql, "'log' AS kind")
	require.Contains(t, sql, "'span' AS kind")
	require.Contains(t, sql, "event_name AS name")
	require.Contains(t, sql, "span_name AS name")
	// Spans have no body: the aligned subquery selects an empty literal.
	require.Contains(t, sql, "'' AS body_preview")
	// Log bodies are truncated server-side.
	require.Contains(t, sql, "leftUTF8(body, 200) AS body_preview")
}

func TestBuildListEventLogQuery_KindFilterDropsOtherTable(t *testing.T) {
	t.Parallel()

	logOnly := ListEventLogParams{
		EventLogFilters: EventLogFilters{
			OrganizationID: "org-under-test",
			TimeStart:      1,
			TimeEnd:        2,
			Kinds:          []string{EventKindLog},
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		CursorTimeUnixNano: 0,
		Limit:              10,
	}
	sql, _, err := buildListEventLogQuery(logOnly)
	require.NoError(t, err)
	require.Contains(t, sql, "otel_logs")
	require.NotContains(t, sql, "otel_traces")
	require.NotContains(t, sql, "UNION ALL")

	spanOnly := logOnly
	spanOnly.Kinds = []string{EventKindSpan}
	sql, _, err = buildListEventLogQuery(spanOnly)
	require.NoError(t, err)
	require.Contains(t, sql, "otel_traces")
	require.NotContains(t, sql, "otel_logs")
}

func TestBuildListEventLogQuery_CursorAddsKeysetPredicate(t *testing.T) {
	t.Parallel()

	params := ListEventLogParams{
		EventLogFilters: EventLogFilters{
			OrganizationID: "org-under-test",
			TimeStart:      1,
			TimeEnd:        2,
			Kinds:          []string{EventKindLog},
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		CursorTimeUnixNano: 0,
		Limit:              10,
	}
	noCursorSQL, _, err := buildListEventLogQuery(params)
	require.NoError(t, err)
	require.NotContains(t, noCursorSQL, "time_unix_nano < ?")

	params.CursorTimeUnixNano = 42
	sql, args, err := buildListEventLogQuery(params)
	require.NoError(t, err)
	require.Contains(t, sql, "time_unix_nano < ?")
	require.Contains(t, args, int64(42))
}

func TestBuildListEventLogQuery_SearchMatchesBodyAndNameForLogsOnly(t *testing.T) {
	t.Parallel()

	sql, args, err := buildListEventLogQuery(ListEventLogParams{
		EventLogFilters: EventLogFilters{
			OrganizationID: "org-under-test",
			TimeStart:      1,
			TimeEnd:        2,
			Kinds:          nil,
			Sources:        nil,
			Names:          nil,
			Search:         "needle",
		},
		CursorTimeUnixNano: 0,
		Limit:              10,
	})
	require.NoError(t, err)
	require.Contains(t, sql, "positionCaseInsensitiveUTF8(body, ?) > 0 OR positionCaseInsensitiveUTF8(event_name, ?) > 0")
	require.Contains(t, sql, "positionCaseInsensitiveUTF8(span_name, ?) > 0")
	// The span subquery must not reference a body column it does not have.
	require.NotContains(t, sql, "positionCaseInsensitiveUTF8(body, ?) > 0 OR positionCaseInsensitiveUTF8(span_name, ?) > 0")
	require.Len(t, args, strings.Count(sql, "?"))
}

func TestBuildCountEventLogQuery_CapsEachTable(t *testing.T) {
	t.Parallel()

	sql, args, err := buildCountEventLogQuery(fullEventLogFilters())
	require.NoError(t, err)
	require.Contains(t, sql, "count() AS total")
	require.Equal(t, 2, strings.Count(sql, "LIMIT 10001"))
	require.Len(t, args, strings.Count(sql, "?"))
}

func TestBuildEventVolumeQuery_BucketsPerKind(t *testing.T) {
	t.Parallel()

	sql, args, err := buildEventVolumeQuery(GetEventVolumeParams{
		EventLogFilters: fullEventLogFilters(),
		IntervalSeconds: 900,
	})
	require.NoError(t, err)
	require.Contains(t, sql, "toStartOfInterval(fromUnixTimestamp64Nano(time_unix_nano), toIntervalSecond(?))")
	require.Contains(t, sql, "'log' AS kind")
	require.Contains(t, sql, "'span' AS kind")
	require.Contains(t, sql, "GROUP BY bucket_ns")
	require.Len(t, args, strings.Count(sql, "?"), "sql: %s\nargs: %#v", sql, args)
	// The interval placeholder leads each subquery's SELECT list, so the
	// interval value must appear before that subquery's filter args.
	require.Equal(t, int64(900), args[0])
}

func TestBuildEventFacetsQuery_UnionsSourcesAndNames(t *testing.T) {
	t.Parallel()

	sql, args, err := buildEventFacetsQuery(EventLogFilters{
		OrganizationID: "org-under-test",
		TimeStart:      1,
		TimeEnd:        2,
		Kinds:          nil,
		Sources:        nil,
		Names:          nil,
		Search:         "",
	})
	require.NoError(t, err)
	require.Contains(t, sql, "'source' AS facet")
	require.Contains(t, sql, "'name' AS facet")
	require.Contains(t, sql, "event_name AS value")
	require.Contains(t, sql, "span_name AS value")
	require.Contains(t, sql, "source != ''")
	require.Contains(t, sql, "event_name != ''")
	require.Contains(t, sql, "span_name != ''")
	require.Contains(t, sql, "SELECT DISTINCT facet, value")
	require.Len(t, args, strings.Count(sql, "?"))
}
