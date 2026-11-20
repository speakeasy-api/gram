// Package aivendorstest provides AI client catalog fixtures for tests.
package aivendorstest

import (
	"slices"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// NewStore returns a store backed by the compiled registry.
func NewStore() *aivendors.Store { return aivendors.NewStore(CompiledSnapshot()) }

// CompiledSnapshot returns one shared snapshot of the compiled registry in
// declaration order, shaped like a database snapshot: no families or aliases.
func CompiledSnapshot() *aivendors.Snapshot { return compiledSnapshot() }

var compiledSnapshot = sync.OnceValue(func() *aivendors.Snapshot {
	return aivendors.NewSnapshot(compiledProducts())
})

// DatabaseSnapshot returns the compiled registry in the order a freshly seeded
// database loads it: products by id, then documents by URL, in byte order.
// aivendors tests prove it equals a real load.
func DatabaseSnapshot() *aivendors.Snapshot { return databaseSnapshot() }

var databaseSnapshot = sync.OnceValue(func() *aivendors.Snapshot {
	products := compiledProducts()
	slices.SortFunc(products, func(a, b aivendors.Product) int { return strings.Compare(a.ID, b.ID) })
	for i := range products {
		slices.SortFunc(products[i].Documents, func(a, b aivendors.Document) int { return strings.Compare(a.URL, b.URL) })
	}
	return aivendors.NewSnapshot(products)
})

func compiledProducts() []aivendors.Product {
	products := aivendors.Products()
	for i := range products {
		products[i].Family = ""
		products[i].Aliases = nil
	}
	return products
}
