// Package aivendors is the single declaration of the AI tools Gram knows: what
// each looks like on a laptop, and which client ID metadata documents it
// publishes. The CIMD admission catalog and the Shadow AI scan catalog are both
// projections of it, so a product becomes admissible and blockable in one edit.
//
// Admission is a prerequisite for blocking: a client absent from the admission
// catalog cannot authenticate in presets mode, so it never reaches a decision.
//
// A leaf package by design — standard library only, so both consumers can
// depend on it without depending on each other.
package aivendors

import "slices"

// Category classifies a product. Mirrors aitargets.Category, duplicated rather
// than imported to keep this package a leaf.
type Category string

const (
	// CategoryHarness is an agentic coding tool or AI IDE.
	CategoryHarness Category = "harness"

	// CategoryAssistant speaks MCP like a harness but is not a coding tool.
	CategoryAssistant Category = "assistant"

	// CategoryLocalModel is an open model run locally.
	CategoryLocalModel Category = "local_model"
)

// Document is one Client ID Metadata Document a product publishes. Several per
// product is normal: stable and insiders builds, two origins, or one per MCP
// server are all the same product to someone deciding whether to allow it.
type Document struct {
	// URL is the client_id the vendor presents. Usually exact; may be a
	// wildcard pattern where the client_id space cannot be enumerated.
	URL string

	// DisplayName is how the admission catalog lists this document —
	// per-document, since a vendor's connector and shared documents differ.
	DisplayName string

	// DisplayOnly marks a document that NAMES a client for the management
	// API but is not itself an admission rule, because another entry — in
	// practice a wildcard covering the same vendor — already admits its URL.
	DisplayOnly bool

	// Enabled gates the document without deleting it.
	Enabled bool
}

// Signatures are the on-device footprints that identify a product. Mirrors
// aitargets.Signatures.
type Signatures struct {
	// BundleIDs are macOS CFBundleIdentifier values.
	BundleIDs []string

	// Binaries are bare command names resolved on the device PATH.
	Binaries []string

	// ConfigDirs mark the tool installed by existing.
	ConfigDirs []string

	// ProcessNames are checked for the running signal.
	ProcessNames []string
}

// IsZero marks a product Gram knows as an OAuth client but never scans for.
func (s Signatures) IsZero() bool {
	return len(s.BundleIDs)+len(s.Binaries)+len(s.ConfigDirs)+len(s.ProcessNames) == 0
}

// Product is one AI tool as a person deciding about it would mean. The unit is
// a product, not a vendor, because a vendor can ship several and a decision
// about one must not reach the others: ChatGPT and Codex share the key
// `openai`, so neither may be identified by it.
type Product struct {
	// ID is what scan reports key on and decisions are recorded against. Set
	// even for unscanned products, so both projections agree on names.
	ID string

	// VendorKey groups products by vendor. Not unique; stable.
	VendorKey string

	DisplayName string

	// Category is required when Signatures are set.
	Category Category

	// Signatures are the on-device footprints, empty for a product Gram knows
	// only as an OAuth client.
	Signatures Signatures

	// VersionPlistKey overrides the Info.plist key read on a bundle match;
	// empty means CFBundleShortVersionString.
	VersionPlistKey string

	// ClientInfoNames are what the product reports at initialize. Attribution
	// only — self-reported, so any client can claim any name.
	ClientInfoNames []string

	// Documents are the CIMD documents it publishes.
	Documents []Document
}

// SpeaksCIMD reports whether a decision about this product is enforceable.
func (p Product) SpeaksCIMD() bool {
	return len(p.Documents) > 0
}

// IsScanned reports whether device agents probe for this product.
func (p Product) IsScanned() bool {
	return !p.Signatures.IsZero()
}

// Products returns the registry in declaration order, which is load-bearing:
// wildcard documents match in the order they appear, so reordering can change
// which entry a client_id is attributed to. A golden test pins it.
func Products() []Product {
	out := make([]Product, 0, len(registry))
	for _, product := range registry {
		out = append(out, product.clone())
	}
	return out
}

// ScanProducts returns the products device agents probe for, in declaration
// order.
func ScanProducts() []Product {
	out := make([]Product, 0, len(registry))
	for _, product := range registry {
		if product.IsScanned() {
			out = append(out, product.clone())
		}
	}
	return out
}

// CIMDDocuments returns every document paired with its publisher, in
// declaration order. The admission catalog is this list, flattened.
func CIMDDocuments() []ProductDocument {
	out := make([]ProductDocument, 0, len(registry))
	for _, product := range registry {
		for _, document := range product.Documents {
			out = append(out, ProductDocument{Product: product.clone(), Document: document})
		}
	}
	return out
}

// ProductDocument pairs a document with its publisher, so a consumer carries
// the vendor key without re-deriving it.
type ProductDocument struct {
	Product  Product
	Document Document
}

// GatewayMatchers is how a product is recognized at Gram's MCP gateway: what
// the server verified, never what a client reported about itself.
type GatewayMatchers struct {
	// VendorKeys is set only when the vendor publishes exactly one product.
	VendorKeys []string

	// ClientIDs are the product's document URLs, matched against the verified
	// client_id and against the catalog entry that admitted it.
	ClientIDs []string
}

// GatewayMatchersFor derives the matchers for one product. A vendor key is
// claimed only when the vendor publishes a single product: blocking Codex by
// `openai` would silently take ChatGPT with it. Deriving that rather than
// remembering it is the point — the hand-written matchers this replaced got
// OpenAI right and Anthropic wrong.
func GatewayMatchersFor(product Product) GatewayMatchers {
	matchers := GatewayMatchers{VendorKeys: nil, ClientIDs: nil}
	if !product.SpeaksCIMD() {
		return matchers
	}
	if soleProductForVendor(product.VendorKey) {
		matchers.VendorKeys = []string{product.VendorKey}
	}
	for _, document := range product.Documents {
		if !document.Enabled {
			continue
		}
		// DisplayOnly URLs are included: naming one literally is what lets
		// the specific product beat the wildcard that also admits it.
		matchers.ClientIDs = append(matchers.ClientIDs, document.URL)
	}
	return matchers
}

// VendorKeys lists every vendor key in the registry, deduplicated and sorted.
func VendorKeys() []string {
	seen := make(map[string]struct{}, len(registry))
	keys := make([]string, 0, len(registry))
	for _, product := range registry {
		if _, ok := seen[product.VendorKey]; ok {
			continue
		}
		seen[product.VendorKey] = struct{}{}
		keys = append(keys, product.VendorKey)
	}
	slices.Sort(keys)
	return keys
}

// soleProductForVendor reports whether exactly one product carries the key.
func soleProductForVendor(vendorKey string) bool {
	count := 0
	for _, product := range registry {
		if product.VendorKey == vendorKey {
			count++
		}
	}
	return count == 1
}

func (p Product) clone() Product {
	out := p
	out.Signatures = Signatures{
		BundleIDs:    slices.Clone(p.Signatures.BundleIDs),
		Binaries:     slices.Clone(p.Signatures.Binaries),
		ConfigDirs:   slices.Clone(p.Signatures.ConfigDirs),
		ProcessNames: slices.Clone(p.Signatures.ProcessNames),
	}
	out.ClientInfoNames = slices.Clone(p.ClientInfoNames)
	out.Documents = slices.Clone(p.Documents)
	return out
}
