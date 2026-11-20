package aivendorstest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
)

func TestCompiledSnapshotMatchesRegistry(t *testing.T) {
	t.Parallel()
	snapshot := aivendorstest.CompiledSnapshot()
	want := aivendors.Products()
	for i := range want {
		want[i].Family = ""
		want[i].Aliases = nil
	}
	require.Equal(t, want, snapshot.Products())
	scanned := aivendors.ScanProducts()
	for i := range scanned {
		scanned[i].Family = ""
		scanned[i].Aliases = nil
	}
	require.Equal(t, scanned, snapshot.ScanProducts())
	require.Same(t, snapshot, aivendorstest.CompiledSnapshot(), "every caller shares one compiled snapshot")
	var documents []aivendors.Preset
	for _, entry := range aivendors.CIMDDocuments() {
		require.Equal(t, strings.Contains(entry.Document.URL, "*"), entry.Document.Pattern, entry.Document.URL)
		documents = append(documents, aivendors.Preset{ProductID: entry.Product.ID, VendorKey: entry.Product.VendorKey, DisplayName: entry.Document.DisplayName, URL: entry.Document.URL, Enabled: entry.Document.Enabled, Pattern: entry.Document.Pattern})
	}
	require.Equal(t, documents, snapshot.Catalog())
	for _, p := range aivendors.Products() {
		require.Equal(t, aivendors.GatewayMatchersFor(p), snapshot.GatewayMatchersFor(p), p.ID)
		require.Equal(t, aivendors.BlockableVendorKey(p.VendorKey), snapshot.BlockableVendorKey(p.VendorKey), p.ID)
	}
}

func TestDatabaseSnapshotIsTheCompiledSnapshotInDatabaseOrder(t *testing.T) {
	t.Parallel()
	compiled := aivendorstest.CompiledSnapshot().Products()
	database := aivendorstest.DatabaseSnapshot().Products()
	require.ElementsMatch(t, ids(compiled), ids(database))
	require.True(t, slices.IsSortedFunc(database, func(a, b aivendors.Product) int { return strings.Compare(a.ID, b.ID) }))
	for _, p := range database {
		require.True(t, slices.IsSortedFunc(p.Documents, func(a, b aivendors.Document) int { return strings.Compare(a.URL, b.URL) }), p.ID)
	}
}

func ids(products []aivendors.Product) []string {
	out := make([]string, 0, len(products))
	for _, p := range products {
		out = append(out, p.ID)
	}
	return out
}
