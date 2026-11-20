package aivendors

import (
	"errors"
	"fmt"
)

// These caps bound one loaded catalog so a bad edit cannot grow every
// reader's in-memory snapshot without limit.
const (
	// MaxProducts caps the live products in one catalog.
	MaxProducts = 500

	// MaxDocumentsPerProduct caps the live documents one product publishes.
	MaxDocumentsPerProduct = 32

	// MaxListEntries caps each signature list and the client info names.
	MaxListEntries = 32
)

// ErrInvalidCatalog is wrapped by every catalog-wide validation failure.
var ErrInvalidCatalog = errors.New("invalid AI client catalog")

// Validate checks the rules every loaded snapshot holds before it is
// published, whatever the consumer: it is not empty, it fits the caps, every
// scanned product has a category, and no two products share a client ID
// space unless one pattern is strictly narrower than the other.
func Validate(s *Snapshot) error {
	if len(s.products) == 0 {
		return fmt.Errorf("%w: no live products", ErrInvalidCatalog)
	}
	if len(s.products) > MaxProducts {
		return fmt.Errorf("%w: %d products exceeds the maximum of %d", ErrInvalidCatalog, len(s.products), MaxProducts)
	}
	for _, p := range s.products {
		if p.IsScanned() && p.Category == "" {
			return fmt.Errorf("%w: product %q has install signatures but no category", ErrInvalidCatalog, p.ID)
		}
		if len(p.Documents) > MaxDocumentsPerProduct {
			return fmt.Errorf("%w: product %q has %d documents, over the maximum of %d", ErrInvalidCatalog, p.ID, len(p.Documents), MaxDocumentsPerProduct)
		}
		for _, list := range [][]string{p.Signatures.BundleIDs, p.Signatures.Binaries, p.Signatures.ConfigDirs, p.Signatures.ProcessNames, p.ClientInfoNames} {
			if len(list) > MaxListEntries {
				return fmt.Errorf("%w: product %q has a list of %d entries, over the maximum of %d", ErrInvalidCatalog, p.ID, len(list), MaxListEntries)
			}
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
			return fmt.Errorf("%w: patterns %q (%s) and %q (%s) overlap and neither is narrower", ErrInvalidCatalog, a.URL, a.ProductID, b.URL, b.ProductID)
		}
	}
	return nil
}
