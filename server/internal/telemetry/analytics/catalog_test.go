package analytics

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultCatalog(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, 2)
	for _, ds := range Default.Datasets() {
		names = append(names, ds.Name)
	}
	require.Equal(t, []string{"sessions", "tool_calls"}, names)

	sessions, ok := Default.Dataset("sessions")
	require.True(t, ok)
	user, ok := sessions.Field("user")
	require.True(t, ok)
	require.True(t, user.Admits("in"))
	require.False(t, user.Admits("sum"), "a dimension admits operators, not aggregations")

	duration, ok := sessions.Field("duration_seconds")
	require.True(t, ok)
	require.True(t, duration.Admits("p95"))
	require.False(t, duration.Admits("equals"), "a measure admits aggregations, not operators")

	_, ok = Default.Dataset("project")
	require.False(t, ok, "project is tenancy, never a dataset or a field")
	_, ok = sessions.Field("project")
	require.False(t, ok)
}

func TestNewCatalogRejectsHalfDeclaredDatasets(t *testing.T) {
	t.Parallel()

	base := func() *Dataset {
		return &Dataset{
			Name:        "things",
			Kind:        KindEvent,
			Grain:       "one row per thing",
			Description: "things",
			TimeExpr:    "started_at",
			Fields: []Field{
				{Name: "thing", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "thing_id"},
			},
			Source: sessionsSource,
		}
	}

	cases := []struct {
		name   string
		mutate func(*Dataset)
		want   string
	}{
		{name: "it rejects a dimension with aggregations", mutate: func(d *Dataset) { d.Fields[0].Aggregations = []Aggregation{AggregationSum} }, want: "must declare operators and no aggregations"},
		{name: "it rejects a measure with operators", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeInt64, Role: RoleMeasure, Unit: "", Operators: equalsIn, Aggregations: []Aggregation{AggregationSum}, Expr: "n"}
		}, want: "must declare aggregations and no operators"},
		{name: "it rejects a string measure", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeString, Role: RoleMeasure, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum}, Expr: "n"}
		}, want: "cannot be a string"},
		{name: "it rejects a duplicate field", mutate: func(d *Dataset) { d.Fields = append(d.Fields, d.Fields[0]) }, want: "twice"},
		{name: "it rejects a field named like the time bucket", mutate: func(d *Dataset) { d.Fields[0].Name = timeBucketColumn }, want: "time bucket"},
		{name: "it rejects a dataset with no source query", mutate: func(d *Dataset) { d.Source = nil }, want: "no source query"},
		{name: "it rejects an unknown kind", mutate: func(d *Dataset) { d.Kind = "table" }, want: "unknown kind"},
		{name: "it rejects a dataset with no grain", mutate: func(d *Dataset) { d.Grain = "" }, want: "no grain"},
		{name: "it rejects an empty dataset name", mutate: func(d *Dataset) { d.Name = "" }, want: "empty name"},
		{name: "it rejects a dataset with no time expression", mutate: func(d *Dataset) { d.TimeExpr = "" }, want: "no time expression"},
		{name: "it rejects a dataset with no fields", mutate: func(d *Dataset) { d.Fields = nil }, want: "declares no fields"},
		{name: "it rejects a field with an empty name", mutate: func(d *Dataset) { d.Fields[0].Name = "" }, want: "empty name or expression"},
		{name: "it rejects a field with an empty expression", mutate: func(d *Dataset) { d.Fields[0].Expr = "" }, want: "empty name or expression"},
		{name: "it rejects an unknown role", mutate: func(d *Dataset) { d.Fields[0].Role = "axis" }, want: "unknown role"},
		{name: "it rejects an unknown field type", mutate: func(d *Dataset) { d.Fields[0].Type = "uuid" }, want: "unknown type"},
		{name: "it rejects an unknown operator", mutate: func(d *Dataset) { d.Fields[0].Operators = []Operator{"like"} }, want: "unknown operator"},
		{name: "it rejects an unknown aggregation", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeInt64, Role: RoleMeasure, Unit: "", Operators: nil, Aggregations: []Aggregation{"median"}, Expr: "n"}
		}, want: "unknown aggregation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds := base()
			tc.mutate(ds)
			_, err := NewCatalog(ds)
			require.ErrorContains(t, err, tc.want)
		})
	}

	t.Run("it rejects a nil dataset instead of panicking", func(t *testing.T) {
		t.Parallel()
		_, err := NewCatalog(nil)
		require.ErrorContains(t, err, "nil dataset")
	})

	t.Run("it rejects two datasets with one name", func(t *testing.T) {
		t.Parallel()
		_, err := NewCatalog(base(), base())
		require.ErrorContains(t, err, "declared twice")
	})
}

func TestSourceQueriesDeduplicateBeforeAggregating(t *testing.T) {
	t.Parallel()

	scope := Scope{OrganizationID: "org", ProjectID: "proj", FromUnixNano: 1, ToUnixNano: 2}

	// What each dataset binds after the scope: the whole list, so a dropped or
	// reordered predicate fails here rather than passing unseen, and a new
	// dataset has to declare its binds before it passes at all.
	eventTypes := make([]any, 0, len(toolCallEventTypes))
	for _, eventType := range toolCallEventTypes {
		eventTypes = append(eventTypes, eventType)
	}
	trailing := map[string][]any{
		Sessions.Name:  {""},                   // session_id != ''
		ToolCalls.Name: append(eventTypes, ""), // event_type IN (...), event_id != ''
	}

	for _, ds := range Default.Datasets() {
		query, args, err := ds.Source(scope).ToSql()
		require.NoError(t, err, ds.Name)
		require.Contains(t, query, "LIMIT 1 BY organization_id, project_id, record_id", ds.Name)
		require.Contains(t, query, "GROUP BY organization_id, project_id", ds.Name)

		tail, declared := trailing[ds.Name]
		require.True(t, declared, "dataset %q declares no expected binds", ds.Name)
		want := append([]any{"org", "proj", int64(1), int64(2)}, tail...)
		require.Equal(t, want, args, ds.Name)
	}
}

// TestToolCallPredicatesShareOneVocabulary: the sessions count and the tool
// call dataset admit the same event types. One is bound by squirrel and the
// other rendered inside an aggregate, so pin that they say the same thing.
func TestToolCallPredicatesShareOneVocabulary(t *testing.T) {
	t.Parallel()

	scope := Scope{OrganizationID: "org", ProjectID: "project", FromUnixNano: 1, ToUnixNano: 2}

	sessions, _, err := sessionsSource(scope).ToSql()
	require.NoError(t, err)
	require.Contains(t, sessions, toolCallEventTypesSQL)
	require.Equal(t, "event_type IN ('tool_call', 'tool_call_result', 'tool_decision')", toolCallEventTypesSQL)

	_, args, err := toolCallsSource(scope).ToSql()
	require.NoError(t, err)
	for _, eventType := range toolCallEventTypes {
		require.Contains(t, args, eventType)
	}
}

// TestDatasetWindowIsBoundedByItsTableRetention: an event dataset reads
// agent_events and a metric dataset agent_metrics, and each may ask for no
// more than its table keeps.
func TestDatasetWindowIsBoundedByItsTableRetention(t *testing.T) {
	t.Parallel()

	require.Equal(t, MaxEventTimeRangeDays, Sessions.MaxTimeRangeDays())
	require.Equal(t, MaxEventTimeRangeDays, ToolCalls.MaxTimeRangeDays())
	require.Equal(t, int64(MaxEventTimeRangeDays)*nanosPerDay, Sessions.MaxTimeRangeNanos())

	metric := &Dataset{Name: "spend", Kind: KindMetric, Grain: "measurement", Description: "", TimeExpr: "t", Fields: nil, Source: nil}
	require.Equal(t, MaxMetricTimeRangeDays, metric.MaxTimeRangeDays())
	require.Equal(t, MaxTimeRangeDays, metric.MaxTimeRangeDays(), "the metric limit is the ceiling")
}

// TestRetentionLimitsMatchTheSchema: the limits restate the tables' TTLs, and
// the schema is the source of truth. If retention changes there, this fails
// here rather than letting the catalog promise a window the table no longer
// keeps.
func TestRetentionLimitsMatchTheSchema(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err, "the analytics package sits under server/, beside the schema")

	ttl := func(table string) int {
		t.Helper()
		// The table's CREATE, then its TTL clause, before the next CREATE.
		pattern := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS ` + table + ` \(.*?TTL [^\n]*INTERVAL (\d+) DAY`)
		match := pattern.FindSubmatch(schema)
		require.NotNil(t, match, "no TTL found for %s", table)
		days, err := strconv.Atoi(string(match[1]))
		require.NoError(t, err)
		return days
	}

	require.Equal(t, MaxEventTimeRangeDays, ttl("agent_events"), "agent_events TTL drifted from the catalog's event limit")
	require.Equal(t, MaxMetricTimeRangeDays, ttl("agent_metrics"), "agent_metrics TTL drifted from the catalog's metric limit")
}
