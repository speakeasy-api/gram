package aivendors

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotExactBeforePatternsAndExplicitPatternFlag(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "broad", VendorKey: "shared", Documents: []Document{{URL: "https://example.com/*/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "specific", VendorKey: "shared", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "exact", VendorKey: "exact", Documents: []Document{{URL: "https://example.com/oauth/exact/client.json", Enabled: true}}},
		{ID: "literal-star", VendorKey: "literal", Documents: []Document{{URL: "https://literal.example.com/*/client.json", Enabled: true, Pattern: false}}},
	}
	snapshot := NewSnapshot(products)
	got, ok := snapshot.CatalogPreset("https://example.com/oauth/exact/client.json")
	require.True(t, ok)
	require.Equal(t, "exact", got.ProductID)
	_, ok = snapshot.CatalogPreset("https://literal.example.com/some-client/client.json")
	require.False(t, ok)
	got, ok = snapshot.CatalogPreset("https://literal.example.com/*/client.json")
	require.True(t, ok)
	require.False(t, got.IsPattern())
	require.False(t, snapshot.BlockableVendorKey("shared"))
	require.True(t, snapshot.BlockableVendorKey("exact"))
	require.Equal(t, snapshot.Fingerprint(), NewSnapshot(products).Fingerprint())
	require.Positive(t, snapshot.Version())
}

func TestSnapshotKeepsInputOrder(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "broad", VendorKey: "broad", Documents: []Document{{URL: "https://example.com/*/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "specific", VendorKey: "specific", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
	}
	forward := NewSnapshot(products)
	got, ok := forward.CatalogPreset("https://example.com/oauth/some-client/client.json")
	require.True(t, ok)
	require.Equal(t, "broad", got.ProductID, "the first matching pattern in input order wins")
	slices.Reverse(products)
	reversed := NewSnapshot(products)
	got, ok = reversed.CatalogPreset("https://example.com/oauth/some-client/client.json")
	require.True(t, ok)
	require.Equal(t, "specific", got.ProductID)
	require.Equal(t, "specific", reversed.Products()[0].ID)
	require.Equal(t, "specific", reversed.Catalog()[0].ProductID)
	require.NotEqual(t, forward.Fingerprint(), reversed.Fingerprint(), "order is content")
}

func TestSnapshotFingerprintTreatsNilAndEmptyAlike(t *testing.T) {
	t.Parallel()
	empty := Product{ID: "p", Signatures: Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{}}, ClientInfoNames: []string{}, Aliases: []Alias{}, Documents: []Document{}}
	require.Equal(t, NewSnapshot([]Product{{ID: "p"}}).Fingerprint(), NewSnapshot([]Product{empty}).Fingerprint())
}

func TestCompiledSnapshotMatchesRegistry(t *testing.T) {
	t.Parallel()
	snapshot := CompiledSnapshot()
	want := Products()
	for i := range want {
		want[i].Family = ""
		want[i].Aliases = nil
	}
	require.Equal(t, want, snapshot.Products())
	scanned := ScanProducts()
	for i := range scanned {
		scanned[i].Family = ""
		scanned[i].Aliases = nil
	}
	require.Equal(t, scanned, snapshot.ScanProducts())
	require.Same(t, snapshot, CompiledSnapshot(), "every caller shares one compiled snapshot")
	var documents []Preset
	for _, entry := range CIMDDocuments() {
		require.Equal(t, strings.Contains(entry.Document.URL, "*"), entry.Document.Pattern, entry.Document.URL)
		documents = append(documents, Preset{ProductID: entry.Product.ID, VendorKey: entry.Product.VendorKey, DisplayName: entry.Document.DisplayName, URL: entry.Document.URL, Enabled: entry.Document.Enabled, Pattern: entry.Document.Pattern})
	}
	require.Equal(t, documents, snapshot.Catalog())
	for _, p := range Products() {
		require.Equal(t, GatewayMatchersFor(p), snapshot.GatewayMatchersFor(p), p.ID)
		require.Equal(t, BlockableVendorKey(p.VendorKey), snapshot.BlockableVendorKey(p.VendorKey), p.ID)
	}
}

func TestSnapshotOwnsAllMutableData(t *testing.T) {
	t.Parallel()
	products := Products()
	snapshot := NewSnapshot(products)
	fingerprint := snapshot.Fingerprint()
	expectedProducts := Products()
	expectedCatalog := slices.Clone(snapshot.Catalog())
	products[0].Documents[0].URL = "https://changed.example.com/input.json"
	copied := snapshot.Products()
	copied[0].Documents[0].URL = "https://changed.example.com/copy.json"
	for i := range copied {
		for _, values := range [][]string{
			copied[i].Signatures.BundleIDs,
			copied[i].Signatures.Binaries,
			copied[i].Signatures.ConfigDirs,
			copied[i].Signatures.ProcessNames,
			copied[i].ClientInfoNames,
		} {
			if len(values) > 0 {
				values[0] = "changed"
			}
		}
		if len(copied[i].Aliases) > 0 {
			copied[i].Aliases[0].Name = "changed"
		}
		if len(copied[i].Documents) > 0 {
			copied[i].Documents[0].DisplayName = "changed"
		}
	}
	presets := snapshot.Catalog()
	presets[0].URL = "https://changed.example.com/preset.json"
	presets[0].Enabled = !presets[0].Enabled
	require.Equal(t, expectedProducts, snapshot.Products())
	require.Equal(t, expectedCatalog, snapshot.Catalog())
	require.Equal(t, fingerprint, NewSnapshot(snapshot.Products()).Fingerprint())
	require.Equal(t, fingerprint, snapshot.Fingerprint())
}
