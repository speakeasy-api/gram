package aivendors

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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

// LastKnownProduct is the product a client ID belonged to before the live catalog stopped admitting it.
type LastKnownProduct struct {
	// ProductID is the product id decisions are recorded against.
	ProductID string

	// DisplayName is the product's name when it was last listed.
	DisplayName string
}

// retiredDocument is a soft-deleted document, or a document of a soft-deleted product.
type retiredDocument struct {
	// ProductID is the product that published the document.
	ProductID string `json:"product_id"`

	// DisplayName is that product's name.
	DisplayName string `json:"display_name"`

	// URL is the exact client ID or pattern.
	URL string `json:"url"`

	// Pattern is the stored document discriminator.
	Pattern bool `json:"pattern"`
}

// Snapshot owns one immutable catalog revision. Accessors return copies.
type Snapshot struct {
	products []Product
	presets  []Preset
	exact    map[string]Preset
	patterns []Preset
	vendors  map[string]int
	// retiredExact and retiredPatterns serve block enforcement only.
	retiredExact    map[string]retiredDocument
	retiredPatterns []retiredDocument
	fingerprint     string
	builtIn         bool
	// skipped are the rows the load that built this snapshot left out.
	skipped []string
	derived sync.Map // values other packages compute once per snapshot
}

// NewSnapshot builds indexes over validated catalog data and takes ownership of
// a deep copy. Input order is kept as listing order; pattern priority depends
// only on content.
func NewSnapshot(products []Product) *Snapshot {
	s := &Snapshot{
		products:        make([]Product, 0, len(products)),
		presets:         nil,
		exact:           make(map[string]Preset),
		patterns:        nil,
		vendors:         make(map[string]int),
		retiredExact:    nil,
		retiredPatterns: nil,
		fingerprint:     "",
		builtIn:         false,
		skipped:         nil,
		derived:         sync.Map{},
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
			if entry.Pattern {
				s.patterns = append(s.patterns, entry)
			} else if current, ok := s.exact[entry.URL]; !ok || !current.Enabled {
				s.exact[entry.URL] = entry
			}
		}
	}
	slices.SortStableFunc(s.patterns, func(a, b Preset) int {
		if c := cmp.Compare(patternSpecificity(b.URL), patternSpecificity(a.URL)); c != 0 {
			return c
		}
		if a.Enabled != b.Enabled {
			if a.Enabled {
				return -1
			}
			return 1
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

// retire indexes removed documents for LastKnown, most recently removed first,
// and folds them into the fingerprint. Malformed patterns are omitted.
func (s *Snapshot) retire(docs []retiredDocument) {
	if len(docs) == 0 {
		return
	}
	s.retiredExact = make(map[string]retiredDocument, len(docs))
	for _, d := range docs {
		if !d.Pattern {
			if _, taken := s.retiredExact[d.URL]; !taken {
				s.retiredExact[d.URL] = d
			}
			continue
		}
		if validatePattern(d.URL) == nil {
			s.retiredPatterns = append(s.retiredPatterns, d)
		}
	}
	slices.SortStableFunc(s.retiredPatterns, func(a, b retiredDocument) int {
		return cmp.Compare(patternSpecificity(b.URL), patternSpecificity(a.URL))
	})
	// retiredDocument contains only JSON-encodable value fields.
	data, _ := json.Marshal(docs)
	hash := sha256.Sum256(append([]byte(s.fingerprint), data...))
	s.fingerprint = hex.EncodeToString(hash[:])
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

// CompiledSnapshot is the built-in registry in database order: products by id, then documents by URL.
func CompiledSnapshot() *Snapshot { return compiledSnapshot() }

var compiledSnapshot = sync.OnceValue(func() *Snapshot { return builtInSnapshot(removals{products: nil, documents: nil}) })

// removals are the built-in rows the database soft-deleted or disabled.
type removals struct {
	products map[string]struct{}
	// documents maps a URL to true when a disabled live row holds it, false when it is deleted.
	documents map[string]bool
}

// builtInSnapshot is the built-in registry in database order with removals applied.
func builtInSnapshot(removed removals) *Snapshot {
	registry := Products()
	products := make([]Product, 0, len(registry))
	for _, p := range registry {
		if _, gone := removed.products[p.ID]; gone {
			continue
		}
		p.Family = ""
		p.Aliases = nil
		var documents []Document
		for _, d := range p.Documents {
			if live, ok := removed.documents[d.URL]; ok {
				if !live {
					continue
				}
				d.Enabled = false
			}
			documents = append(documents, d)
		}
		p.Documents = documents
		slices.SortFunc(p.Documents, func(a, b Document) int { return strings.Compare(a.URL, b.URL) })
		products = append(products, p)
	}
	slices.SortFunc(products, func(a, b Product) int { return strings.Compare(a.ID, b.ID) })
	s := NewSnapshot(products)
	s.builtIn = true
	return s
}

// BuiltIn reports whether this is the built-in catalog rather than a database load.
func (s *Snapshot) BuiltIn() bool { return s.builtIn }

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
// most literal path segments. Ties go to an enabled document, then the lower
// URL, then product ID, so input order never decides attribution. A disabled
// document still wins its match: its client is unknown, not a broader
// pattern's product.
func (s *Snapshot) CatalogPreset(clientID string) (Preset, bool) {
	none := Preset{ProductID: "", VendorKey: "", DisplayName: "", URL: "", Enabled: false, Pattern: false}
	if p, ok := s.exact[clientID]; ok {
		if !p.Enabled {
			return none, false
		}
		return p, true
	}
	for _, p := range s.patterns {
		if matchesPattern(p.URL, clientID) {
			if !p.Enabled {
				return none, false
			}
			return p, true
		}
	}
	return Preset{ProductID: "", VendorKey: "", DisplayName: "", URL: "", Enabled: false, Pattern: false}, false
}

// LastKnown resolves a client ID the live catalog does not admit to the
// product that last published it, for block enforcement only. Live attribution
// wins: an enabled live document returns false, a disabled one returns its
// product, and only a client ID no live document matches falls through to
// removed documents, exact URLs first, then the most specific pattern.
func (s *Snapshot) LastKnown(clientID string) (LastKnownProduct, bool) {
	none := LastKnownProduct{ProductID: "", DisplayName: ""}
	live, matched := s.exact[clientID]
	if !matched {
		for _, p := range s.patterns {
			if matchesPattern(p.URL, clientID) {
				live, matched = p, true
				break
			}
		}
	}
	if matched {
		if live.Enabled {
			return none, false
		}
		for _, p := range s.products {
			if p.ID == live.ProductID {
				return LastKnownProduct{ProductID: p.ID, DisplayName: p.DisplayName}, true
			}
		}
		return none, false
	}
	if d, ok := s.retiredExact[clientID]; ok {
		return LastKnownProduct{ProductID: d.ProductID, DisplayName: d.DisplayName}, true
	}
	for _, d := range s.retiredPatterns {
		if matchesPattern(d.URL, clientID) {
			return LastKnownProduct{ProductID: d.ProductID, DisplayName: d.DisplayName}, true
		}
	}
	return none, false
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

// Derived computes a value once per snapshot and caller-owned key; callers must not mutate it.
func Derived[K comparable, T any](s *Snapshot, key K, compute func(*Snapshot) T) T {
	v, ok := s.derived.Load(key)
	if !ok {
		v, _ = s.derived.LoadOrStore(key, sync.OnceValue(func() T { return compute(s) }))
	}
	get, ok := v.(func() T)
	if !ok {
		panic(fmt.Sprintf("aivendors: derived value for key %T is %T, not the requested type", key, v))
	}
	return get()
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
	reported := make(map[string]struct{}, len(snapshot.skipped))
	for _, row := range snapshot.skipped {
		reported[row] = struct{}{}
	}
	s := &Store{refreshMu: sync.Mutex{}, reported: reported, confirmed: atomic.Int64{}, mu: sync.RWMutex{}, snapshot: snapshot}
	s.confirmed.Store(time.Now().UnixNano())
	return s
}

// Snapshot captures one catalog revision.
func (s *Store) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

type pinnedSnapshotKey struct{ store *Store }

// Pin captures one snapshot for the rest of a request, keeping one already pinned.
func (s *Store) Pin(ctx context.Context) context.Context {
	if _, ok := ctx.Value(pinnedSnapshotKey{store: s}).(*Snapshot); ok {
		return ctx
	}
	return context.WithValue(ctx, pinnedSnapshotKey{store: s}, s.Snapshot())
}

// SnapshotFor returns the snapshot pinned on ctx, or captures the current one.
func (s *Store) SnapshotFor(ctx context.Context) *Snapshot {
	if snapshot, ok := ctx.Value(pinnedSnapshotKey{store: s}).(*Snapshot); ok {
		return snapshot
	}
	return s.Snapshot()
}

// Middleware pins one snapshot on each request so every reader in it sees the same revision.
func (s *Store) Middleware(next http.Handler) http.Handler {
	if s == nil {
		panic("aivendors: catalog middleware registered without a store")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(s.Pin(r.Context())))
	})
}

// Publish replaces the current snapshot unless next has the same content; it always replaces the built-in one.
func (s *Store) Publish(next *Snapshot) {
	if next == nil {
		panic("aivendors: publish requires a snapshot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.builtIn || s.snapshot.Fingerprint() != next.Fingerprint() {
		s.snapshot = next
	}
}
