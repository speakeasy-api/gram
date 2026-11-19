package aivendors

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.Equal(t, snapshot.Fingerprint(), NewSnapshot(products).Fingerprint())
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

func TestSnapshotKeepsInputOrder(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "broad", VendorKey: "broad", Documents: []Document{{URL: "https://example.com/*/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "specific", VendorKey: "specific", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
	}
	forward := NewSnapshot(products)
	got, ok := forward.CatalogPreset("https://example.com/oauth/some-client/client.json")
	require.True(t, ok)
	require.Equal(t, "specific", got.ProductID, "the more specific pattern wins even when listed second")
	require.Equal(t, "broad", forward.Products()[0].ID)
	require.Equal(t, "broad", forward.Catalog()[0].ProductID)
	slices.Reverse(products)
	reversed := NewSnapshot(products)
	got, ok = reversed.CatalogPreset("https://example.com/oauth/some-client/client.json")
	require.True(t, ok)
	require.Equal(t, "specific", got.ProductID)
	require.Equal(t, "specific", reversed.Products()[0].ID)
	require.Equal(t, "specific", reversed.Catalog()[0].ProductID)
	require.NotEqual(t, forward.Fingerprint(), reversed.Fingerprint(), "listing order is content")
}

func TestSnapshotPatternPriorityIgnoresInputOrder(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "broad", Documents: []Document{{URL: "https://example.com/*/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "specific", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "exact", Documents: []Document{{URL: "https://example.com/oauth/exact/client.json", Enabled: true}}},
		{ID: "tenant-b", Documents: []Document{{URL: "https://example.com/*/tenant/client.json", Enabled: true, Pattern: true}}},
		{ID: "tenant-a", Documents: []Document{{URL: "https://example.com/*/tenant/client.json?v=1", Enabled: true, Pattern: true}}},
	}
	want := map[string]string{
		"https://example.com/oauth/some-client/client.json": "specific",
		"https://example.com/oauth/exact/client.json":       "exact",
		"https://example.com/other/some-client/client.json": "broad",
		"https://example.com/oauth/tenant/client.json":      "tenant-b",
		"https://example.com/other/tenant/client.json":      "tenant-b",
		"https://example.com/other/tenant/client.json?v=1":  "tenant-a",
		"https://example.com/oauth/client.json":             "",
	}
	reference := NewSnapshot(products)
	orders := [][]Product{slices.Clone(products), slices.Clone(products)}
	slices.Reverse(orders[1])
	orders = append(orders, append(slices.Clone(products[2:]), products[:2]...))
	for i, order := range orders {
		snapshot := NewSnapshot(order)
		for clientID, productID := range want {
			got, ok := snapshot.CatalogPreset(clientID)
			require.Equal(t, productID != "", ok, "order %d: %s", i, clientID)
			require.Equal(t, productID, got.ProductID, "order %d: %s", i, clientID)
			ref, _ := reference.CatalogPreset(clientID)
			require.Equal(t, ref, got, "order %d: %s", i, clientID)
		}
	}
}

func TestSnapshotIdenticalPatternsPreferLowerProductID(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "vendor-b", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "vendor-a", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true}}},
	}
	for range 2 {
		got, ok := NewSnapshot(products).CatalogPreset("https://example.com/oauth/some-client/client.json")
		require.True(t, ok)
		require.Equal(t, "vendor-a", got.ProductID)
		slices.Reverse(products)
	}
}

func TestSnapshotDisabledDocumentsDoNotShadow(t *testing.T) {
	t.Parallel()
	products := []Product{
		{ID: "specific", Documents: []Document{{URL: "https://example.com/oauth/*/client.json", Enabled: false, Pattern: true}}},
		{ID: "broad", Documents: []Document{{URL: "https://example.com/*/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "exact", Documents: []Document{{URL: "https://example.com/oauth/exact/client.json", Enabled: false}}},
	}
	snapshot := NewSnapshot(products)
	for _, clientID := range []string{"https://example.com/oauth/some-client/client.json", "https://example.com/oauth/exact/client.json"} {
		got, ok := snapshot.CatalogPreset(clientID)
		require.True(t, ok, clientID)
		require.Equal(t, "broad", got.ProductID, clientID)
	}
	require.Len(t, snapshot.Catalog(), 3, "disabled documents are still listed")
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

func TestWireVersionStaysInDeviceRange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prefix uint32
		want   int32
	}{
		{name: "zero", prefix: 0, want: 1},
		{name: "sign bit only", prefix: 1 << 31, want: 1},
		{name: "one", prefix: 1, want: 1},
		{name: "sign bit and one", prefix: 1<<31 | 1, want: 1},
		{name: "max int32", prefix: math.MaxInt32, want: math.MaxInt32},
		{name: "all ones", prefix: math.MaxUint32, want: math.MaxInt32},
		{name: "ordinary", prefix: 0x1234_5678, want: 0x1234_5678},
		{name: "ordinary with sign bit", prefix: 0x9234_5678, want: 0x1234_5678},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, wireVersion(tc.prefix))
		})
	}
}

func TestCompiledSnapshotVersionIsDerivedFromContent(t *testing.T) {
	t.Parallel()
	compiled := CompiledSnapshot()
	require.GreaterOrEqual(t, compiled.Version(), int32(1))
	require.LessOrEqual(t, compiled.Version(), int32(math.MaxInt32))

	rebuilt := Products()
	for i := range rebuilt {
		rebuilt[i].Family = ""
		rebuilt[i].Aliases = nil
	}
	independent := NewSnapshot(rebuilt)
	require.Equal(t, compiled.Version(), independent.Version(), "same content in the same order")
	require.Equal(t, compiled.Fingerprint(), independent.Fingerprint())

	rebuilt[0].DisplayName += " renamed"
	changed := NewSnapshot(rebuilt)
	require.NotEqual(t, compiled.Version(), changed.Version())
	require.NotEqual(t, compiled.Fingerprint(), changed.Fingerprint())
}
