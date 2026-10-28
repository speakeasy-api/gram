// Package aivendors is the single declaration of the AI tools Speakeasy knows: what
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

import (
	"slices"
	"strings"
)

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

	// Enabled gates the document without deleting it.
	Enabled bool

	// IDJAG admits the document's client to the ID-JAG grant. It applies only
	// while Enabled, and only to an exact URL: pattern documents are
	// deliberately never accepted, since their client_id space is unbounded.
	IDJAG bool
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

// IsZero marks a product Speakeasy knows as an OAuth client but never scans for.
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

	// Signatures are the on-device footprints, empty for a product Speakeasy knows
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
// A product whose documents are all disabled does NOT speak CIMD for this
// purpose: admission skips those rows, so no caller can be resolved through
// them, and reporting the product as CIMD-capable would let a de-admitted
// vendor be classified as enforceable.
func (p Product) SpeaksCIMD() bool {
	return hasEnabledDocument(p)
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

// IDJAGDocuments returns the enabled, exact documents whose clients may
// exchange an ID-JAG, in declaration order.
func IDJAGDocuments() []ProductDocument {
	var out []ProductDocument
	for _, entry := range CIMDDocuments() {
		if entry.Document.Enabled && entry.Document.IDJAG && !strings.Contains(entry.Document.URL, "*") {
			out = append(out, entry)
		}
	}
	return out
}

// IDJAGDocument returns the ID-JAG document whose URL is exactly clientID.
func IDJAGDocument(clientID string) (ProductDocument, bool) {
	for _, entry := range IDJAGDocuments() {
		if entry.Document.URL == clientID {
			return entry, true
		}
	}
	var none ProductDocument
	return none, false
}

// IDJAGProductForDocument returns the product that publishes documentURL when
// that product also has an ID-JAG document, so a client presenting another of
// its documents can be told to update.
func IDJAGProductForDocument(documentURL string) (Product, bool) {
	for _, entry := range IDJAGDocuments() {
		for _, document := range entry.Product.Documents {
			if document.URL == documentURL {
				return entry.Product, true
			}
		}
	}
	var none Product
	return none, false
}

// ProductDocument pairs a document with its publisher, so a consumer carries
// the vendor key without re-deriving it.
type ProductDocument struct {
	Product  Product
	Document Document
}

// GatewayMatchers is how a product is recognized at Speakeasy's MCP gateway: what
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
	// The key is also only worth claiming when some document is enabled: with
	// none, no catalog entry can admit the product, so the key would report a
	// target as enforceable that the gateway can never resolve.
	if soleProductForVendor(product.VendorKey) && hasEnabledDocument(product) {
		matchers.VendorKeys = []string{product.VendorKey}
	}
	for _, document := range product.Documents {
		if !document.Enabled {
			continue
		}
		matchers.ClientIDs = append(matchers.ClientIDs, document.URL)
	}
	return matchers
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

// hasEnabledDocument reports whether any of the product's CIMD documents is
// still admitted.
func hasEnabledDocument(product Product) bool {
	for _, document := range product.Documents {
		if document.Enabled {
			return true
		}
	}
	return false
}

// BlockableVendorKey reports whether a vendor key names something the gateway
// could actually resolve a caller to: a vendor the registry knows, publishing
// exactly one product, with at least one admitted document. It is the same
// rule GatewayMatchersFor applies to a built-in, exported so an organization
// writing its own matcher is held to it too — otherwise a custom target
// claiming `anthropic` would block every Anthropic product at once.
func BlockableVendorKey(vendorKey string) bool {
	if !soleProductForVendor(vendorKey) {
		return false
	}
	for _, product := range registry {
		if product.VendorKey == vendorKey {
			return product.SpeaksCIMD() && hasEnabledDocument(product)
		}
	}
	return false
}
