package analytics

import (
	"os"
	"regexp"
	"slices"
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
	require.Equal(t, []string{"tool_calls", "skills", "risk_findings"}, names)

	toolCalls, ok := Default.Dataset("tool_calls")
	require.True(t, ok)
	user, ok := toolCalls.Field("user")
	require.True(t, ok)
	require.True(t, user.AdmitsOperator("in"))
	require.False(t, user.AdmitsAggregation("sum"), "a dimension admits operators, not a measure's aggregations")
	require.True(t, user.AdmitsAggregation("count_distinct"), "the one aggregation a dimension declares is over its own values")
	require.False(t, user.AdmitsOperator("count_distinct"), "an aggregation is not a filter operator")

	for _, name := range []string{"tool_name", "mcp_server", "session", "user"} {
		field, ok := toolCalls.Field(name)
		require.True(t, ok, name)
		require.True(t, field.AdmitsAggregation("count_distinct"), "%s answers a distinct count", name)
	}
	status, ok := toolCalls.Field("status")
	require.True(t, ok)
	require.False(t, status.AdmitsAggregation("count_distinct"), "a dimension that does not declare it does not admit it")
	_, ok = toolCalls.Field("skill")
	require.False(t, ok, "a Skill call is an ordinary tool call here; skills are their own dataset")

	skills, ok := Default.Dataset("skills")
	require.True(t, ok)
	require.Equal(t, "skill invocation", skills.Grain)
	require.Contains(t, skills.Description, "Claude Code", "the one dataset a single producer family fills says so")
	skill, ok := skills.Field("skill")
	require.True(t, ok)
	require.Equal(t, RoleDimension, skill.Role)
	require.True(t, skill.Default, "the skills view opens broken down by skill")
	require.True(t, skill.AdmitsOperator("in"))
	require.True(t, skill.AdmitsAggregation("count_distinct"), "skills used is a distinct count")
	require.Empty(t, skill.Description, "the producer caveat is on the dataset, not the field")
	for _, name := range []string{"session", "user", "surface", "status", "duration_ms"} {
		_, ok := skills.Field(name)
		require.True(t, ok, "%s reads on a skill invocation as on any call", name)
	}

	duration, ok := toolCalls.Field("duration_ms")
	require.True(t, ok)
	require.True(t, duration.AdmitsAggregation("p95"))
	require.False(t, duration.AdmitsOperator("equals"), "a measure admits aggregations, not operators")

	_, ok = Default.Dataset("project")
	require.False(t, ok, "project is tenancy, never a dataset or a field")
	_, ok = toolCalls.Field("project")
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
			Access:      AccessProject,
			Fields: []Field{
				{Name: "thing", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "thing_id", Description: ""},
			},
			Source: toolCallsSource,
		}
	}

	cases := []struct {
		name   string
		mutate func(*Dataset)
		want   string
	}{
		{name: "it rejects a numeric dimension", mutate: func(d *Dataset) { d.Fields[0].Type = TypeInt64 }, want: "must be a string"},
		{name: "it rejects a dimension with a measure's aggregation", mutate: func(d *Dataset) { d.Fields[0].Aggregations = []Aggregation{AggregationSum} }, want: "count_distinct and no other aggregation"},
		{name: "it rejects a dimension with no operators", mutate: func(d *Dataset) { d.Fields[0].Operators = nil }, want: "must declare operators"},
		{name: "it names an aggregation a dimension declares that nothing knows", mutate: func(d *Dataset) { d.Fields[0].Aggregations = []Aggregation{"count_distinctt"} }, want: `unknown aggregation "count_distinctt"`},
		{name: "it rejects a measure declaring count_distinct", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationCountDistinct}, Expr: "n", Description: ""}
		}, want: "cannot declare count_distinct"},
		{name: "it rejects a measure with operators", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: equalsIn, Aggregations: []Aggregation{AggregationSum}, Expr: "n", Description: ""}
		}, want: "must declare aggregations and no operators"},
		{name: "it rejects a string measure", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeString, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum}, Expr: "n", Description: ""}
		}, want: "cannot be a string"},
		{name: "it rejects a duplicate field", mutate: func(d *Dataset) { d.Fields = append(d.Fields, d.Fields[0]) }, want: "twice"},
		{name: "it rejects a field named like the time bucket", mutate: func(d *Dataset) { d.Fields[0].Name = timeBucketColumn }, want: "time bucket"},
		{name: "it rejects a dataset with no source query", mutate: func(d *Dataset) { d.Source = Source{} }, want: "no source query"},
		{name: "it rejects an unknown kind", mutate: func(d *Dataset) { d.Kind = "table" }, want: "unknown kind"},
		{name: "it rejects a dataset with no grain", mutate: func(d *Dataset) { d.Grain = "" }, want: "no grain"},
		{name: "it rejects an empty dataset name", mutate: func(d *Dataset) { d.Name = "" }, want: "empty name"},
		{name: "it rejects a dataset with no time expression", mutate: func(d *Dataset) { d.TimeExpr = "" }, want: "no time expression"},
		{name: "it rejects a dataset that does not say who may read it", mutate: func(d *Dataset) { d.Access = "" }, want: "unknown access"},
		{name: "it rejects a lookup fallback with no lookup", mutate: func(d *Dataset) { d.Fields[0].LookupFallback = "''" }, want: "lookup fallback but no lookup"},
		{name: "it rejects a dataset with no fields", mutate: func(d *Dataset) { d.Fields = nil }, want: "declares no fields"},
		{name: "it rejects a field with an empty name", mutate: func(d *Dataset) { d.Fields[0].Name = "" }, want: "empty name or expression"},
		{name: "it rejects a field with an empty expression", mutate: func(d *Dataset) { d.Fields[0].Expr = "" }, want: "empty name or expression"},
		{name: "it rejects an unknown role", mutate: func(d *Dataset) { d.Fields[0].Role = "axis" }, want: "unknown role"},
		{name: "it rejects an unknown field type", mutate: func(d *Dataset) { d.Fields[0].Type = "uuid" }, want: "unknown type"},
		{name: "it rejects an unknown operator", mutate: func(d *Dataset) { d.Fields[0].Operators = []Operator{"like"} }, want: "unknown operator"},
		{name: "it rejects an unknown aggregation", mutate: func(d *Dataset) {
			d.Fields[0] = Field{Name: "n", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{"median"}, Expr: "n", Description: ""}
		}, want: "unknown aggregation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds := base()
			tc.mutate(ds)
			_, err := NewCatalog(nil, ds)
			require.ErrorContains(t, err, tc.want)
		})
	}

	t.Run("it accepts a dimension declaring only count_distinct", func(t *testing.T) {
		t.Parallel()
		ds := base()
		ds.Fields[0].Aggregations = countDistinct
		_, err := NewCatalog(nil, ds)
		require.NoError(t, err)
	})

	t.Run("it rejects a nil dataset instead of panicking", func(t *testing.T) {
		t.Parallel()
		_, err := NewCatalog(nil, nil)
		require.ErrorContains(t, err, "nil dataset")
	})

	t.Run("it rejects two datasets with one name", func(t *testing.T) {
		t.Parallel()
		_, err := NewCatalog(nil, base(), base())
		require.ErrorContains(t, err, "declared twice")
	})

	t.Run("it rejects a dimension reading through an undeclared lookup", func(t *testing.T) {
		t.Parallel()
		ds := base()
		ds.Fields[0].Lookup = "display_names"
		_, err := NewCatalog(nil, ds)
		require.ErrorContains(t, err, `undeclared lookup "display_names"`)

		_, err = NewCatalog([]*Lookup{{Name: "display_names", Description: "names", Load: nil}}, ds)
		require.NoError(t, err)
	})

	t.Run("it rejects a measure reading through a lookup", func(t *testing.T) {
		t.Parallel()
		ds := base()
		ds.Fields[0] = Field{Name: "n", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum}, Expr: "n", Description: "", Lookup: "display_names"}
		_, err := NewCatalog([]*Lookup{{Name: "display_names", Description: "names", Load: nil}}, ds)
		require.ErrorContains(t, err, "cannot read through a lookup")
	})

	t.Run("it rejects a lookup with no name or declared twice", func(t *testing.T) {
		t.Parallel()
		_, err := NewCatalog([]*Lookup{{Name: "", Description: "", Load: nil}}, base())
		require.ErrorContains(t, err, "lookup with no name")
		_, err = NewCatalog([]*Lookup{{Name: "a", Description: "", Load: nil}, {Name: "a", Description: "", Load: nil}}, base())
		require.ErrorContains(t, err, `lookup "a" declared twice`)
	})
}

func TestSourceQueriesNeverSortTheWindow(t *testing.T) {
	t.Parallel()

	qc := QueryContext{Tenant: Tenant{OrganizationID: "org", ProjectID: "proj"}, Window: Window{FromUnixNano: 1, ToUnixNano: 2}}

	for _, ds := range []*Dataset{ToolCalls, Skills} {
		query, args, err := ds.Source.Rows(qc).ToSql()
		require.NoError(t, err, ds.Name)
		require.NotContains(t, query, "LIMIT 1 BY", ds.Name)
		require.Contains(t, query, "FROM tool_calls WHERE", ds.Name)
		require.Contains(t, query, "GROUP BY organization_id, project_id, event_id HAVING started_at >= ? AND started_at < ?", ds.Name)
		require.Contains(t, query, "event_id IN (SELECT event_id FROM tool_calls WHERE organization_id = ? AND project_id = ? AND started_at_unix_nano >= ? AND started_at_unix_nano < ?)", ds.Name)
		require.Equal(t, []any{"org", "proj", "org", "proj", int64(1), int64(2), int64(1), int64(2)}, args, ds.Name)
	}
}

// TestDatasetWindowIsBoundedByItsTableRetention: an event dataset reads
// agent_events and a metric dataset agent_metrics, and each may ask for no
// more than its table keeps.
func TestDatasetWindowIsBoundedByItsTableRetention(t *testing.T) {
	t.Parallel()

	require.Equal(t, MaxEventTimeRangeDays, ToolCalls.MaxTimeRangeDays())
	require.Equal(t, MaxEventTimeRangeDays, Skills.MaxTimeRangeDays())
	require.Equal(t, int64(MaxEventTimeRangeDays)*nanosPerDay, ToolCalls.MaxTimeRangeNanos())

	metric := &Dataset{Name: "spend", Kind: KindMetric, Grain: "measurement", Description: "", TimeExpr: "t", Fields: nil, Source: Source{}}
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
	require.Equal(t, RiskFindings.MaxTimeRangeDays(), ttl("risk_findings"), "risk_findings TTL drifted from the risk_findings dataset's limit")
}

// TestDimensionValuesSkipTheCollapse: a filter bar lists every dimension's
// values on page load, so none of them may sort the window.
func TestDimensionValuesSkipTheCollapse(t *testing.T) {
	t.Parallel()

	tenant := Tenant{OrganizationID: "org", ProjectID: "proj"}
	for _, ds := range Default.Datasets() {
		// Only agent_events keeps raw observations to list from. A finding's
		// later copies can withdraw it, so its values come from the live rows.
		if ds.Source.Table.planSuffix() != "agent_events" {
			continue
		}
		for _, field := range ds.Fields {
			if field.Role != RoleDimension {
				continue
			}
			require.NotEmpty(t, field.Typeahead, "%s.%s falls back to the collapsed rows", ds.Name, field.Name)
			plan, err := CompileValues(Default, tenant, nil, ValuesRequest{Dataset: ds.Name, Dimension: field.Name, FromUnixNano: 1, ToUnixNano: 2, Limit: 0})
			require.NoError(t, err)
			require.NotContains(t, plan.SQL, "LIMIT 1 BY", "%s.%s", ds.Name, field.Name)
		}
	}
}

func TestDimensionValuesWithoutTypeaheadReadTheRows(t *testing.T) {
	t.Parallel()

	ds := *ToolCalls
	ds.Fields = slices.Clone(ToolCalls.Fields)
	for i := range ds.Fields {
		ds.Fields[i].Typeahead = ""
	}
	plan, err := CompileValues(MustCatalog(Lookups, &ds), Tenant{OrganizationID: "org", ProjectID: "proj"}, nil, ValuesRequest{Dataset: ds.Name, Dimension: "tool_name", FromUnixNano: 1, ToUnixNano: 2, Limit: 0})
	require.NoError(t, err)
	require.Contains(t, plan.SQL, "SELECT tool_name AS value, count() AS n FROM (")
	require.Contains(t, plan.SQL, "FROM tool_calls WHERE")
}
