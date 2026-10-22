package aivendors

import (
	"github.com/stretchr/testify/require"
	"slices"
	"testing"
)

func TestSnapshotSpecificityAndExplicitPatternFlag(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "broad", VendorKey: "shared", Documents: []Document{{URL: "https://example.com/*/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "specific", VendorKey: "shared", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "exact", VendorKey: "exact", Documents: []Document{{URL: "https://example.com/oauth/exact/client.json", Enabled: true}}},
		{ID: "literal-star", VendorKey: "literal", Documents: []Document{{URL: "https://literal.example.com/*/client.json", Enabled: true, Pattern: false}}},
	}
	snapshot := NewSnapshot(products)
	got, ok := snapshot.CatalogPreset("https://example.com/oauth/some-client/client.json")
	require.True(t, ok)
	require.Equal(t, "specific", got.ProductID)
	got, ok = snapshot.CatalogPreset("https://example.com/oauth/exact/client.json")
	require.True(t, ok)
	require.Equal(t, "exact", got.ProductID)
	_, ok = snapshot.CatalogPreset("https://literal.example.com/some-client/client.json")
	require.False(t, ok)
	got, ok = snapshot.CatalogPreset("https://literal.example.com/*/client.json")
	require.True(t, ok)
	require.False(t, got.IsPattern())
	require.False(t, snapshot.BlockableVendorKey("shared"))
	require.True(t, snapshot.BlockableVendorKey("exact"))
	slices.Reverse(products)
	require.Equal(t, snapshot.Fingerprint(), NewSnapshot(products).Fingerprint())
	require.Equal(t, snapshot.Version(), NewSnapshot(products).Version())
	require.Positive(t, snapshot.Version())
}

func TestSnapshotEqualSpecificityIsStable(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "second", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "first", Documents: []Document{{URL: "https://example.com/*/tenant/client.json", Enabled: true, Pattern: true}}},
	}
	first := NewSnapshot(products)
	slices.Reverse(products)
	second := NewSnapshot(products)
	a, ok := first.CatalogPreset("https://example.com/oauth/tenant/client.json")
	require.True(t, ok)
	b, ok := second.CatalogPreset("https://example.com/oauth/tenant/client.json")
	require.True(t, ok)
	require.Equal(t, "first", a.ProductID)
	require.Equal(t, a, b)
}

func TestSnapshotOwnsAllMutableData(t *testing.T) {
	t.Parallel()
	products := Products()
	snapshot := NewSnapshot(products)
	fingerprint := snapshot.Fingerprint()
	products[0].Documents[0].URL = "https://changed.example.com/client.json"
	copied := snapshot.Products()
	copied[0].Documents = nil
	for i := range copied {
		if len(copied[i].Signatures.BundleIDs) > 0 {
			copied[i].Signatures.BundleIDs[0] = "changed"
		}
	}
	presets := snapshot.Catalog()
	presets[0].Enabled = false
	require.Equal(t, fingerprint, NewSnapshot(snapshot.Products()).Fingerprint())
	require.Equal(t, fingerprint, snapshot.Fingerprint())
}
