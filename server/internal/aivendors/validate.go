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
	for i, a := range s.patterns {
		for _, b := range s.patterns[i+1:] {
			if a.ProductID == b.ProductID || !PatternsOverlap(a.URL, b.URL) {
				continue
			}
			if a.URL != b.URL && (patternWithin(a.URL, b.URL) || patternWithin(b.URL, a.URL)) {
				continue
			}
			return fmt.Errorf("%w: patterns %q (%s) and %q (%s) overlap and neither is narrower", errInvalidCatalog, a.URL, a.ProductID, b.URL, b.ProductID)
		}
	}
	return nil
}
