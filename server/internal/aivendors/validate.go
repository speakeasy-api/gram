package aivendors

import (
	"errors"
	"fmt"
)

const (
	// aitargets bounds only scanned products; this bounds the rest at about ten times today's 53.
	maxProducts = 500

	// Nothing else bounds a product's documents; today's most is 2.
	maxDocumentsPerProduct = 32

	// aitargets bounds scanned products' client info names at 16; this holds every product to the same.
	maxClientInfoNames = 16

	// The overlap check compares every pair of enabled patterns; today there are 4.
	maxPatterns = 64

	// maxRemovedDocuments bounds the removed documents one load reads to keep
	// blocks applying, newest first. It is the most documents a catalog within
	// the caps can hold, so removing every product and document of one such
	// catalog never reaches it.
	maxRemovedDocuments = maxProducts * maxDocumentsPerProduct
)

var errInvalidCatalog = errors.New("invalid AI client catalog")

// validateCatalog holds every loaded snapshot to the rules no consumer validator checks.
func validateCatalog(s *Snapshot) error {
	if len(s.products) == 0 {
		return fmt.Errorf("%w: no live products", errInvalidCatalog)
	}
	if len(s.products) > maxProducts {
		return fmt.Errorf("%w: %d products exceeds the maximum of %d", errInvalidCatalog, len(s.products), maxProducts)
	}
	for _, p := range s.products {
		if len(p.Documents) > maxDocumentsPerProduct {
			return fmt.Errorf("%w: product %q has %d documents, over the maximum of %d", errInvalidCatalog, p.ID, len(p.Documents), maxDocumentsPerProduct)
		}
		if len(p.ClientInfoNames) > maxClientInfoNames {
			return fmt.Errorf("%w: product %q has %d client info names, over the maximum of %d", errInvalidCatalog, p.ID, len(p.ClientInfoNames), maxClientInfoNames)
		}
	}
	// A disabled pattern makes its matches unknown, and an unknown result cannot be ambiguous between products.
	enabled := make([]Preset, 0, len(s.patterns))
	for _, p := range s.patterns {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) > maxPatterns {
		return fmt.Errorf("%w: %d enabled patterns exceeds the maximum of %d", errInvalidCatalog, len(enabled), maxPatterns)
	}
	parsed := make([]parsedPattern, len(enabled))
	for i, p := range enabled {
		parsed[i], _ = parsePattern(p.URL)
	}
	for i, a := range enabled {
		for j := i + 1; j < len(enabled); j++ {
			b := enabled[j]
			if a.ProductID == b.ProductID || !parsed[i].overlaps(parsed[j]) {
				continue
			}
			if a.URL != b.URL && (parsed[i].within(parsed[j]) || parsed[j].within(parsed[i])) {
				continue
			}
			return fmt.Errorf("%w: patterns %q (%s) and %q (%s) overlap and neither is narrower", errInvalidCatalog, a.URL, a.ProductID, b.URL, b.ProductID)
		}
	}
	return nil
}
