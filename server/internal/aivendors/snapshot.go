package aivendors

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"sync"
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
	version     int32
}

// NewSnapshot builds indexes over validated catalog data and takes ownership of
// a deep copy. Database readers validate rows before calling it.
func NewSnapshot(products []Product) *Snapshot {
	s := &Snapshot{
		products:    make([]Product, 0, len(products)),
		presets:     nil,
		exact:       make(map[string]Preset),
		patterns:    nil,
		vendors:     make(map[string]int),
		fingerprint: "",
		version:     0,
	}
	for _, p := range products {
		p = p.clone()
		// Canonical ordering makes fingerprints independent of query ordering.
		slices.SortFunc(p.Documents, func(a, b Document) int { return strings.Compare(a.URL, b.URL) })
		s.products = append(s.products, p)
	}
	slices.SortFunc(s.products, func(a, b Product) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range s.products {
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
	slices.SortFunc(s.patterns, func(a, b Preset) int {
		if c := cmp.Compare(patternSpecificity(b.URL), patternSpecificity(a.URL)); c != 0 {
			return c
		}
		if c := strings.Compare(a.URL, b.URL); c != 0 {
			return c
		}
		return strings.Compare(a.ProductID, b.ProductID)
	})
	// Product contains only JSON-encodable value fields.
	data, _ := json.Marshal(s.products)
	hash := sha256.Sum256(data)
	s.fingerprint = hex.EncodeToString(hash[:])
	// The wire contract is a positive int32; zero denotes device fallback.
	// This is an opaque identifier, not an ordered or collision-free revision.
	const versionMask = uint32(1<<31 - 1)
	s.version = int32(binary.BigEndian.Uint32(hash[:4]) & versionMask)
	if s.version == 0 {
		s.version = 1
	}
	return s
}

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

// Fingerprint identifies the complete snapshot without truncation.
func (s *Snapshot) Fingerprint() string { return s.fingerprint }

// Version is the positive, opaque wire identifier echoed by device agents.
func (s *Snapshot) Version() int32 { return s.version }

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

// CatalogPreset resolves exact URLs before patterns, then most literal segments
// first. Equal specificity uses URL and product ID for deterministic attribution.
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

	mu       sync.RWMutex
	snapshot *Snapshot
}

// NewStore requires a successfully loaded initial snapshot before serving.
func NewStore(snapshot *Snapshot) *Store {
	if snapshot == nil {
		panic("AI catalog requires an initial snapshot")
	}
	return &Store{refreshMu: sync.Mutex{}, mu: sync.RWMutex{}, snapshot: snapshot}
}

// Snapshot captures one catalog revision.
func (s *Store) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}
