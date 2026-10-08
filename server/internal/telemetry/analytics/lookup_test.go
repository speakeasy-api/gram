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

// TestLoadLookupsLoadsWhatTheDatasetReads: one load per lookup the dataset's
// fields name, however many fields share it; a lookup no field reads is not
// loaded; a declaration without a loader folds nothing; an unknown dataset
// loads nothing and a failing loader fails the load.
func TestLoadLookupsLoadsWhatTheDatasetReads(t *testing.T) {
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

	maps, err := bound.LoadLookups(ctx, tenant, "things")
	require.NoError(t, err)
	require.Equal(t, LookupMaps{"names": {"a": "A"}}, maps)
	require.Equal(t, 1, namesLoads, "two fields read one lookup: one load")
	require.Equal(t, 0, unusedLoads, "a lookup no field reads is not loaded")

	maps, err = bound.LoadLookups(ctx, tenant, "nothing")
	require.NoError(t, err)
	require.Nil(t, maps, "an unknown dataset loads nothing; the compiler reports it")

	maps, err = lookupTestCatalog(t).LoadLookups(ctx, tenant, "things")
	require.NoError(t, err)
	require.Empty(t, maps, "a declaration without a loader folds nothing")

	failing, err := lookupTestCatalog(t).WithLoaders(map[string]LookupLoader{
		"names":  func(context.Context, Tenant) (map[string]string, error) { return nil, errors.New("boom") },
		"unused": func(context.Context, Tenant) (map[string]string, error) { return nil, nil },
	})
	require.NoError(t, err)
	_, err = failing.LoadLookups(ctx, tenant, "things")
	require.ErrorContains(t, err, "load lookup names: boom")
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
	require.Equal(t, LookupMaps{}, must(Default.LoadLookups(t.Context(), Tenant{OrganizationID: "org", ProjectID: "project"}, ToolCalls.Name)), "the bare catalog loads nothing, which is what widget validation plans against")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
