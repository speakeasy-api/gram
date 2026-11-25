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

// LastKnownProduct is the product a client ID belonged to before the live
// catalog stopped admitting it, and the document that matched it.
type LastKnownProduct struct {
	// ProductID is the product id decisions are recorded against.
	ProductID string `json:"product_id"`

	// VendorKey is the product's vendor key.
	VendorKey string `json:"vendor_key"`

	// DisplayName is the product's name when it was last listed.
	DisplayName string `json:"display_name"`

	// Category is the product's category when it was last listed.
	Category Category `json:"category"`

	// URL is the matching document: an exact client ID or a pattern.
	URL string `json:"url"`
}

// removedDocument is a soft-deleted document, or a document of a soft-deleted product.
type removedDocument struct {
	LastKnownProduct

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
	// removedExact, removedPatterns and removedProducts only keep blocks applying.
	removedExact    map[string]removedDocument
	removedPatterns []removedDocument
	removedProducts map[string]LastKnownProduct
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
		removedExact:    nil,
		removedPatterns: nil,
		removedProducts: nil,
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

// keepRemoved indexes removed documents for LastKnown, most recently removed
// first, and folds them into the fingerprint. Malformed patterns are omitted.
func (s *Snapshot) keepRemoved(docs []removedDocument) {
	if len(docs) == 0 {
		return
	}
	live := make(map[string]struct{}, len(s.products))
	for _, p := range s.products {
		live[p.ID] = struct{}{}
	}
	s.removedExact = make(map[string]removedDocument, len(docs))
	s.removedProducts = make(map[string]LastKnownProduct)
	for _, d := range docs {
		if d.Pattern && validatePattern(d.URL) != nil {
			continue
		}
		if d.Pattern {
			s.removedPatterns = append(s.removedPatterns, d)
		} else if _, taken := s.removedExact[d.URL]; !taken {
			s.removedExact[d.URL] = d
		}
		if _, isLive := live[d.ProductID]; !isLive {
			if _, seen := s.removedProducts[d.ProductID]; !seen {
				s.removedProducts[d.ProductID] = d.LastKnownProduct
			}
		}
	}
	slices.SortStableFunc(s.removedPatterns, func(a, b removedDocument) int {
		return cmp.Compare(patternSpecificity(b.URL), patternSpecificity(a.URL))
	})
	// removedDocument contains only JSON-encodable value fields.
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

// match finds the live document that decides a client ID: exact URLs before
// patterns, then the pattern with the most literal path segments. Ties go to
// an enabled document, then the lower URL, then product ID, so input order
// never decides attribution.
func (s *Snapshot) match(clientID string) (Preset, bool) {
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

// CatalogPreset resolves the enabled live document for a client ID. A disabled
// document still wins its match: its client is unknown, not a broader
// pattern's product.
func (s *Snapshot) CatalogPreset(clientID string) (Preset, bool) {
	p, ok := s.match(clientID)
	if !ok || !p.Enabled {
		return Preset{ProductID: "", VendorKey: "", DisplayName: "", URL: "", Enabled: false, Pattern: false}, false
	}
	return p, true
}

// LastKnown resolves a client ID the live catalog does not admit to the
// product that last published it. It is read only to keep blocks applying.
// Live attribution wins: an enabled live document returns false, a disabled
// one returns its product, and only a client ID no live document matches
// falls through to removed documents, exact URLs first, then the most
// specific pattern.
func (s *Snapshot) LastKnown(clientID string) (LastKnownProduct, bool) {
	if p, ok := s.match(clientID); ok {
		if p.Enabled {
			return LastKnownProduct{ProductID: "", VendorKey: "", DisplayName: "", Category: "", URL: ""}, false
		}
		product := s.product(p.ProductID)
		return LastKnownProduct{ProductID: p.ProductID, VendorKey: p.VendorKey, DisplayName: product.DisplayName, Category: product.Category, URL: p.URL}, true
	}
	if d, ok := s.removedExact[clientID]; ok {
		return d.LastKnownProduct, true
	}
	for _, d := range s.removedPatterns {
		if matchesPattern(d.URL, clientID) {
			return d.LastKnownProduct, true
		}
	}
	return LastKnownProduct{ProductID: "", VendorKey: "", DisplayName: "", Category: "", URL: ""}, false
}

// RemovedProduct reports a product removed from the catalog whose documents
// still keep blocks applying, as it was last listed.
func (s *Snapshot) RemovedProduct(id string) (LastKnownProduct, bool) {
	p, ok := s.removedProducts[id]
	return p, ok
}

// DocumentsDisabled reports a live product whose documents are all disabled,
// so blocks on it apply through those documents but nothing admits it.
func (s *Snapshot) DocumentsDisabled(id string) bool {
	p := s.product(id)
	return p.ID != "" && len(p.Documents) > 0 && !hasEnabledDocument(p)
}

// product returns the live product with id, or the zero product.
func (s *Snapshot) product(id string) Product {
	for _, p := range s.products {
		if p.ID == id {
			return p
		}
	}
	return Product{ID: "", VendorKey: "", Family: "", DisplayName: "", Category: "", Signatures: Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil}, VersionPlistKey: "", ClientInfoNames: nil, Aliases: nil, Documents: nil}
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
