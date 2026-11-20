package aivendors

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Preset identifies a document and the product that publishes it.
type Preset struct {
	// ProductID is the stable product identifier used by access decisions.
	ProductID string

	// VendorKey groups products from one vendor.
	VendorKey string

	// DisplayName names the document in the admission catalog.
	DisplayName string

	// URL is the exact client ID or path-segment pattern.
	URL string

	// Enabled determines whether this document admits callers.
	Enabled bool

	// Pattern is the explicit database discriminator, never inferred from URL.
	Pattern bool
}

// IsPattern reports the stored document discriminator.
func (p Preset) IsPattern() bool { return p.Pattern }

// Snapshot owns one immutable catalog revision. Accessors return copies.
type Snapshot struct {
	products    []Product
	presets     []Preset
	exact       map[string]Preset
	patterns    []Preset
	vendors     map[string]int
	fingerprint string
}

// NewSnapshot builds indexes over validated catalog data and takes ownership of
// a deep copy. Input order is kept as listing order; pattern priority depends
// only on content.
func NewSnapshot(products []Product) *Snapshot {
	s := &Snapshot{
		products:    make([]Product, 0, len(products)),
		presets:     nil,
		exact:       make(map[string]Preset),
		patterns:    nil,
		vendors:     make(map[string]int),
		fingerprint: "",
	}
	for _, p := range products {
		p = p.clone()
		s.products = append(s.products, p)
		s.vendors[p.VendorKey]++
		for _, d := range p.Documents {
			entry := Preset{
				ProductID:   p.ID,
				VendorKey:   p.VendorKey,
				DisplayName: d.DisplayName,
				URL:         d.URL,
				Enabled:     d.Enabled,
				Pattern:     d.Pattern,
			}
			s.presets = append(s.presets, entry)
			if !entry.Enabled {
				continue
			}
			if entry.Pattern {
				s.patterns = append(s.patterns, entry)
			} else {
				s.exact[entry.URL] = entry
			}
		}
	}
	slices.SortStableFunc(s.patterns, func(a, b Preset) int {
		if c := cmp.Compare(patternSpecificity(b.URL), patternSpecificity(a.URL)); c != 0 {
			return c
		}
		if c := strings.Compare(a.URL, b.URL); c != 0 {
			return c
		}
		return strings.Compare(a.ProductID, b.ProductID)
	})
	canonical := make([]Product, 0, len(s.products))
	for _, p := range s.products {
		canonical = append(canonical, p.withEmptyAsNil())
	}
	// Product contains only JSON-encodable value fields.
	data, _ := json.Marshal(canonical)
	hash := sha256.Sum256(data)
	s.fingerprint = hex.EncodeToString(hash[:])
	return s
}

// patternSpecificity counts a pattern's literal path segments.
func patternSpecificity(pattern string) int {
	_, rest, _ := splitAuthorityAndPath(pattern)
	path, _ := splitPathAndSuffix(rest)
	n := 0
	for segment := range strings.SplitSeq(path, "/") {
		if segment != "*" {
			n++
		}
	}
	return n
}

// withEmptyAsNil makes nil and empty slices encode the same in the fingerprint.
func (p Product) withEmptyAsNil() Product {
	orNil := func(v []string) []string {
		if len(v) == 0 {
			return nil
		}
		return v
	}
	p.Signatures.BundleIDs = orNil(p.Signatures.BundleIDs)
	p.Signatures.Binaries = orNil(p.Signatures.Binaries)
	p.Signatures.ConfigDirs = orNil(p.Signatures.ConfigDirs)
	p.Signatures.ProcessNames = orNil(p.Signatures.ProcessNames)
	p.ClientInfoNames = orNil(p.ClientInfoNames)
	if len(p.Aliases) == 0 {
		p.Aliases = nil
	}
	if len(p.Documents) == 0 {
		p.Documents = nil
	}
	return p
}

// CompiledSnapshot returns the process's one immutable snapshot of the compiled
// registry, shaped like a database snapshot: families and aliases stay with the
// package-level registry.
func CompiledSnapshot() *Snapshot { return compiledSnapshot() }

var compiledSnapshot = sync.OnceValue(func() *Snapshot {
	products := Products()
	for i := range products {
		products[i].Family = ""
		products[i].Aliases = nil
	}
	return NewSnapshot(products)
})

// Fingerprint hashes the complete snapshot, including product and document
// order, which is listing order.
func (s *Snapshot) Fingerprint() string { return s.fingerprint }

// ScanProducts returns isolated copies of the live products devices scan for.
func (s *Snapshot) ScanProducts() []Product {
	out := make([]Product, 0, len(s.products))
	for _, p := range s.products {
		if p.IsScanned() {
			out = append(out, p.clone())
		}
	}
	return out
}

// Products returns isolated copies of the live products.
func (s *Snapshot) Products() []Product {
	out := make([]Product, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, p.clone())
	}
	return out
}

// Catalog returns enabled and disabled live documents.
func (s *Snapshot) Catalog() []Preset { return slices.Clone(s.presets) }

// CatalogPreset resolves exact URLs before patterns, then the pattern with the
// most literal path segments. Ties go to the lower URL, then product ID, so
// input order never decides attribution.
func (s *Snapshot) CatalogPreset(clientID string) (Preset, bool) {
	if p, ok := s.exact[clientID]; ok {
		return p, true
	}
	for _, p := range s.patterns {
		if matchesPattern(p.URL, clientID) {
			return p, true
		}
	}
	return Preset{ProductID: "", VendorKey: "", DisplayName: "", URL: "", Enabled: false, Pattern: false}, false
}

// BlockableVendorKey requires exactly one live product and an enabled document.
func (s *Snapshot) BlockableVendorKey(key string) bool {
	if s.vendors[key] != 1 {
		return false
	}
	for _, p := range s.products {
		if p.VendorKey == key {
			return p.SpeaksCIMD()
		}
	}
	return false
}

// GatewayMatchersFor derives verified matchers using this snapshot's vendor set.
func (s *Snapshot) GatewayMatchersFor(p Product) GatewayMatchers {
	out := GatewayMatchers{VendorKeys: nil, ClientIDs: nil}
	if !p.SpeaksCIMD() {
		return out
	}
	if s.vendors[p.VendorKey] == 1 {
		out.VendorKeys = []string{p.VendorKey}
	}
	for _, d := range p.Documents {
		if d.Enabled {
			out.ClientIDs = append(out.ClientIDs, d.URL)
		}
	}
	return out
}

// Store publishes immutable snapshots. A compound operation calls Snapshot once
// and uses that view throughout; refreshing never mutates a captured view.
type Store struct {
	refreshMu sync.Mutex
	// reported holds the skipped rows already logged, guarded by refreshMu.
	reported map[string]struct{}
	// confirmed is when the served snapshot last matched the database, in Unix nanoseconds.
	confirmed atomic.Int64

	mu       sync.RWMutex
	snapshot *Snapshot
}

// NewStore requires a successfully loaded initial snapshot before serving.
func NewStore(snapshot *Snapshot) *Store {
	if snapshot == nil {
		panic("AI catalog requires an initial snapshot")
	}
	s := &Store{refreshMu: sync.Mutex{}, reported: nil, confirmed: atomic.Int64{}, mu: sync.RWMutex{}, snapshot: snapshot}
	s.confirmed.Store(time.Now().UnixNano())
	return s
}

// Snapshot captures one catalog revision.
func (s *Store) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}
