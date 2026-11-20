package aivendors

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompiledRegistryPassesValidation(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateCatalog(NewSnapshot(Products())))
}

func TestValidateRejectsCatalogShapes(t *testing.T) {
	t.Parallel()
	pattern := func(id, vendor, url string, enabled bool) Product {
		return Product{ID: id, VendorKey: vendor, Documents: []Document{{URL: url, Enabled: enabled, Pattern: true}}}
	}
	many := make([]Product, 0, maxProducts+1)
	for i := range maxProducts + 1 {
		many = append(many, Product{ID: fmt.Sprintf("p-%d", i), VendorKey: "v"})
	}
	docs := make([]Document, 0, maxDocumentsPerProduct+1)
	for i := range maxDocumentsPerProduct + 1 {
		docs = append(docs, Document{URL: fmt.Sprintf("https://example.com/%d/client.json", i), Enabled: true})
	}
	names := make([]string, maxClientInfoNames+1)
	for i := range names {
		names[i] = strings.Repeat("n", i+1)
	}
	for _, tc := range []struct {
		name     string
		products []Product
		want     string
	}{
		{name: "empty", products: nil, want: "no live products"},
		{name: "too many products", products: many, want: "products exceeds"},
		{name: "too many documents", products: []Product{{ID: "x", VendorKey: "x", Documents: docs}}, want: "documents"},
		{name: "too many client info names", products: []Product{{ID: "x", VendorKey: "x", ClientInfoNames: names}}, want: "client info names"},
		{name: "incomparable overlap", products: []Product{
			pattern("a", "a", "https://example.com/oauth/*/client.json", true),
			pattern("b", "b", "https://example.com/*/fixed/client.json", true),
		}, want: "overlap"},
		{name: "same pattern on two products of one vendor", products: []Product{
			pattern("a", "shared", "https://example.com/oauth/*/client.json", true),
			pattern("b", "shared", "https://example.com/oauth/*/client.json", true),
		}, want: "overlap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateCatalog(NewSnapshot(tc.products))
			require.ErrorIs(t, err, errInvalidCatalog)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestValidateAllowsNarrowerAndUnrelatedPatterns(t *testing.T) {
	t.Parallel()
	pattern := func(id, url string, enabled bool) Product {
		return Product{ID: id, VendorKey: id, Documents: []Document{{URL: url, Enabled: enabled, Pattern: true}}}
	}
	for _, tc := range []struct {
		name     string
		products []Product
	}{
		{name: "strictly narrower", products: []Product{
			pattern("broad", "https://example.com/*/*/client.json", true),
			pattern("narrow", "https://example.com/oauth/*/client.json", true),
		}},
		{name: "one product", products: []Product{{ID: "one", VendorKey: "one", Documents: []Document{
			{URL: "https://example.com/oauth/*/client.json", Enabled: true, Pattern: true},
			{URL: "https://example.com/*/fixed/client.json", Enabled: true, Pattern: true},
		}}}},
		{name: "disabled overlap", products: []Product{
			pattern("a", "https://example.com/oauth/*/client.json", true),
			pattern("b", "https://example.com/*/fixed/client.json", false),
		}},
		{name: "different segment counts", products: []Product{
			pattern("a", "https://example.com/oauth/*/client.json", true),
			pattern("b", "https://example.com/oauth/codex/*/client.json", true),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateCatalog(NewSnapshot(tc.products)))
		})
	}
}

func TestPatternWithin(t *testing.T) {
	t.Parallel()
	require.True(t, patternWithin("https://example.com/oauth/*/client.json", "https://example.com/*/*/client.json"))
	require.False(t, patternWithin("https://example.com/*/*/client.json", "https://example.com/oauth/*/client.json"))
	require.True(t, patternWithin("https://example.com/oauth/*/client.json", "https://example.com/oauth/*/client.json"))
	require.False(t, patternWithin("https://example.com/oauth/*/client.json", "https://example.com/*/fixed/client.json"))
	require.False(t, patternWithin("https://example.com/oauth/*/client.json", "https://other.example.com/oauth/*/client.json"))
}

func TestValidateCapsEnabledPatterns(t *testing.T) {
	t.Parallel()
	products := make([]Product, 0, maxPatterns+1)
	for i := range maxPatterns + 1 {
		products = append(products, Product{ID: fmt.Sprintf("p-%d", i), VendorKey: "v", Documents: []Document{{URL: fmt.Sprintf("https://p%d.example.com/*/client.json", i), Enabled: true, Pattern: true}}})
	}
	err := validateCatalog(NewSnapshot(products))
	require.ErrorIs(t, err, errInvalidCatalog)
	require.ErrorContains(t, err, "enabled patterns")
}
