package analytics

import (
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

	t.Run("it rejects two datasets with one name", func(t *testing.T) {
		t.Parallel()
		_, err := NewCatalog(base(), base())
		require.ErrorContains(t, err, "declared twice")
	})
}

func TestSourceQueriesDeduplicateBeforeAggregating(t *testing.T) {
	t.Parallel()

	scope := Scope{OrganizationID: "org", ProjectID: "proj", FromUnixNano: 1, ToUnixNano: 2}
	for _, ds := range Default.Datasets() {
		query, args, err := ds.Source(scope).ToSql()
		require.NoError(t, err, ds.Name)
		require.Contains(t, query, "LIMIT 1 BY organization_id, project_id, record_id", ds.Name)
		require.Contains(t, query, "GROUP BY organization_id, project_id", ds.Name)
		require.Equal(t, []any{"org", "proj", int64(1), int64(2)}, args[:4], ds.Name)
	}
}
