package analytics

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// lookupTestCatalog declares two lookups and a dataset whose two dimensions
// read through the first, so loading can be watched per lookup.
func lookupTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := NewCatalog(
		[]*Lookup{
			{Name: "names", Description: "display names", Load: nil},
			{Name: "unused", Description: "read by no field", Load: nil},
		},
		&Dataset{
			Name:        "things",
			Kind:        KindEvent,
			Grain:       "thing",
			Description: "things",
			TimeExpr:    "started_at",
			Fields: []Field{
				{Name: "thing", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "thing_id", Description: "", Lookup: "names"},
				{Name: "other", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "other_id", Description: "", Lookup: "names"},
				{Name: "plain", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "plain", Description: "", Lookup: ""},
			},
			Source: toolCallsSource,
		},
	)
	require.NoError(t, err)
	return catalog
}

// TestWithLoadersBindsEveryDeclaredLookup: the service must supply a loader
// for every lookup the catalog declares and nothing else, and binding leaves
// the declaration untouched.
func TestWithLoadersBindsEveryDeclaredLookup(t *testing.T) {
	t.Parallel()
	catalog := lookupTestCatalog(t)
	load := func(context.Context, Tenant) (map[string]string, error) { return nil, nil }

	_, err := catalog.WithLoaders(map[string]LookupLoader{"names": load})
	require.ErrorContains(t, err, `lookup "unused" has no loader`)

	_, err = catalog.WithLoaders(map[string]LookupLoader{"names": load, "unused": load, "extra": load})
	require.ErrorContains(t, err, `undeclared lookup "extra"`)

	bound, err := catalog.WithLoaders(map[string]LookupLoader{"names": load, "unused": load})
	require.NoError(t, err)
	for _, l := range bound.Lookups() {
		require.NotNil(t, l.Load, l.Name)
	}
	for _, l := range catalog.Lookups() {
		require.Nil(t, l.Load, "the declaration stays loader-less")
	}
	require.Equal(t, catalog.Datasets(), bound.Datasets())
}

// TestLoadLookupsLoadsWhatTheRequestReads: one load per lookup the read
// fields name, however many of them share it; a lookup read by no requested
// field is not loaded, so a bare count neither pays for it nor can fail on
// it; a declaration without a loader folds nothing; an unknown dataset loads
// nothing and a failing loader fails the load.
func TestLoadLookupsLoadsWhatTheRequestReads(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tenant := Tenant{OrganizationID: "org", ProjectID: "project"}

	var namesLoads, unusedLoads int
	bound, err := lookupTestCatalog(t).WithLoaders(map[string]LookupLoader{
		"names": func(_ context.Context, got Tenant) (map[string]string, error) {
			namesLoads++
			require.Equal(t, tenant, got)
			return map[string]string{"a": "A"}, nil
		},
		"unused": func(context.Context, Tenant) (map[string]string, error) {
			unusedLoads++
			return nil, nil
		},
	})
	require.NoError(t, err)

	maps, err := bound.LoadLookups(ctx, tenant, "things", []string{"thing", "other", "plain"})
	require.NoError(t, err)
	require.Equal(t, LookupMaps{"names": {"a": "A"}}, maps)
	require.Equal(t, 1, namesLoads, "two fields read one lookup: one load")
	require.Equal(t, 0, unusedLoads, "a lookup no field reads is not loaded")

	maps, err = bound.LoadLookups(ctx, tenant, "things", []string{"plain"})
	require.NoError(t, err)
	require.Empty(t, maps, "a request that reads no folded field loads nothing")
	require.Equal(t, 1, namesLoads)

	maps, err = bound.LoadLookups(ctx, tenant, "things", nil)
	require.NoError(t, err)
	require.Empty(t, maps, "a bare count reads no field and loads nothing")
	require.Equal(t, 1, namesLoads)

	maps, err = bound.LoadLookups(ctx, tenant, "nothing", []string{"thing"})
	require.NoError(t, err)
	require.Nil(t, maps, "an unknown dataset loads nothing; the compiler reports it")

	maps, err = lookupTestCatalog(t).LoadLookups(ctx, tenant, "things", []string{"thing"})
	require.NoError(t, err)
	require.Empty(t, maps, "a declaration without a loader folds nothing")

	failing, err := lookupTestCatalog(t).WithLoaders(map[string]LookupLoader{
		"names":  func(context.Context, Tenant) (map[string]string, error) { return nil, errors.New("boom") },
		"unused": func(context.Context, Tenant) (map[string]string, error) { return nil, nil },
	})
	require.NoError(t, err)
	_, err = failing.LoadLookups(ctx, tenant, "things", []string{"thing"})
	require.ErrorContains(t, err, "load lookup names: boom")
	_, err = failing.LoadLookups(ctx, tenant, "things", []string{"plain"})
	require.NoError(t, err, "a failing store cannot fail a request that does not read through it")
}

// TestRequestReads: a request reads its dimensions, the fields its measures
// aggregate and the fields it filters on, and a count with no field reads
// nothing.
func TestRequestReads(t *testing.T) {
	t.Parallel()

	req := Request{
		Dataset:      "tool_calls",
		FromUnixNano: 0,
		ToUnixNano:   0,
		Grain:        "",
		Dimensions:   []string{"mcp_server"},
		Measures:     []Measure{{Op: "count", Field: "", Alias: ""}, {Op: "count_distinct", Field: "tool", Alias: ""}},
		Filters:      []Filter{{Field: "status", Operator: "equals", Values: []string{"ok"}}},
		OrderBy:      nil,
		Limit:        0,
		Ungrouped:    false,
	}
	require.Equal(t, []string{"mcp_server", "tool", "status"}, req.Reads())

	count := Request{Dataset: "tool_calls", FromUnixNano: 0, ToUnixNano: 0, Grain: "", Dimensions: nil, Measures: []Measure{{Op: "count", Field: "", Alias: ""}}, Filters: nil, OrderBy: nil, Limit: 0, Ungrouped: false}
	require.Empty(t, count.Reads())
}

// TestDefaultCatalogLookups: every lookup the v1 catalog declares is read by
// a field, and describe can find it by name.
func TestDefaultCatalogLookups(t *testing.T) {
	t.Parallel()

	read := map[string]bool{}
	for _, ds := range Default.Datasets() {
		for _, f := range ds.Fields {
			if f.Lookup != "" {
				read[f.Lookup] = true
				_, ok := Default.Lookup(f.Lookup)
				require.True(t, ok, "%s.%s reads through %q", ds.Name, f.Name, f.Lookup)
			}
		}
	}
	for _, l := range Default.Lookups() {
		require.True(t, read[l.Name], "lookup %q is read by no field", l.Name)
		require.Nil(t, l.Load, "the declaration carries no loader; the service attaches it")
		require.NotEmpty(t, l.Description)
	}
	require.Equal(t, LookupMaps{}, must(Default.LoadLookups(t.Context(), Tenant{OrganizationID: "org", ProjectID: "project"}, ToolCalls.Name, []string{"mcp_server"})), "the bare catalog loads nothing, which is what widget validation plans against")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
