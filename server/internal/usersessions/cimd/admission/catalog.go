package admission

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// Preset is one curated entry in Gram's CIMD client catalog: a vendor's
// published Client ID Metadata Document, identified by the URL that
// vendor's client presents as its client_id.
//
// Matching is exact string equality by default, and never origin matching.
// An origin allowlist would be far broader than intended — "claude.ai" as
// an origin covers every path on the host, where the catalog trusts exactly
// two documents under it. The one relaxation is a path-segment wildcard for
// vendors whose client_id namespace cannot be enumerated, which still
// cannot widen the host; see pattern.go.
type Preset struct {
	// VendorKey groups documents published by the same vendor. Not unique:
	// a vendor may ship several clients (Claude Code and Claude, stable and
	// insiders builds). Stable — it is the key a future per-vendor opt-out
	// table would reference, so renaming one is a breaking change.
	VendorKey string

	// DisplayName is the human-readable client name shown in the dashboard
	// and returned by the listPresets management endpoint.
	DisplayName string

	// URL is the client_id the vendor's client presents. Normally exact:
	// it must equal the `client_id` member of the document served at this
	// URL (draft-02 §4), verified out-of-band before the entry is added.
	//
	// It may instead be a wildcard PATTERN (see pattern.go) for the small
	// number of vendors that mint one document per connector or install, so
	// their client_id space cannot be enumerated. Patterns are restricted
	// to this compile-time catalog and can never widen the host.
	URL string

	// DisplayOnly marks an entry that NAMES a client for the management API
	// but is not itself an admission rule, because another entry — in
	// practice a wildcard covering the same vendor — already admits its URL.
	//
	// It exists so the two states stay honest. Without it, an operator could
	// set Enabled=false on such a row, see it reported as disabled by
	// listPresets, and still have the URL admitted by the overlapping
	// pattern. A DisplayOnly row is excluded from matching entirely, so the
	// wildcard is unambiguously the rule and Enabled means what it says on
	// every row that participates.
	DisplayOnly bool

	// Enabled gates the entry without deleting it. A disabled entry is
	// inert for admission but still listed by the management API, so an
	// operator can see that Gram knows about the vendor and has chosen not
	// to admit it. Disabling an entry immediately de-admits it on every
	// presets-mode issuer at deploy, so it is only for pulling an entry
	// that turns out to be wrong. Meaningless on a DisplayOnly row, which
	// never participates in matching.
	Enabled bool
}

// IsPattern reports whether this entry's URL is a wildcard pattern rather
// than a literal client_id. Surfaced through the management API so the
// dashboard can render a glob as such instead of offering it as a URL a
// client would present verbatim.
//
// A DisplayOnly entry is never a pattern: it names one concrete URL that
// some other entry admits.
func (p Preset) IsPattern() bool {
	return isPattern(p.URL)
}

// catalog is Gram's curated preset list. Issuers in ModePresets — which is
// the default for any issuer that has never had a mode explicitly set —
// accept every enabled entry here, with no per-issuer rows and no customer
// action required. Adding a vendor extends every presets-mode issuer on
// deploy; that implicit membership is the documented contract, and is what
// makes "Gram trusts Claude Code" a thing an operator gets for free.
//
// The flip side: a MISSING entry is a hard, unrecoverable auth failure for
// that client. MCP clients pick CIMD over dynamic client registration once,
// at metadata-discovery time, and do not fall back to registration when
// /authorize rejects the client_id — the end user sees an OAuth error with
// no recourse. Recovery is operator-side only (a catalog addition, or an
// issuer custom URL). Be generous here; an extra entry costs a string
// comparison, a missing one costs a support ticket.
//
// Every entry must be verified live before it lands: HTTP 200, valid JSON,
// `client_id` exactly equal to URL, and `token_endpoint_auth_method` of
// "none" (Gram's validator accepts public clients only). Record the
// verification date in the comment above each vendor block.
// catalog is Gram's curated preset list, derived from the aivendors registry
// so a vendor's documents and on-device signatures are declared once. Issuers
// in ModePresets accept every enabled entry, so adding a vendor extends every
// presets-mode issuer on deploy. The verification bar and the ordering rules
// live with the registry; see internal/aivendors.
var catalog = buildCatalog()

func buildCatalog() []Preset {
	documents := aivendors.CIMDDocuments()
	presets := make([]Preset, 0, len(documents))
	for _, entry := range documents {
		presets = append(presets, Preset{
			VendorKey:   entry.Product.VendorKey,
			DisplayName: entry.Document.DisplayName,
			URL:         entry.Document.URL,
			DisplayOnly: entry.Document.DisplayOnly,
			Enabled:     entry.Document.Enabled,
		})
	}
	return presets
}

// catalogURLs and catalogPatterns index the enabled catalog entries for the
// admission hot path, which runs on the unauthenticated /authorize surface
// once per URL-shaped client_id. Built once at init: the catalog is a
// compile-time constant, so there is nothing to invalidate.
//
// The split is what keeps wildcards cheap. Every ordinary client_id
// resolves in one map lookup and never touches the pattern list; only a
// miss walks the handful of patterns.
var catalogURLs, catalogPatterns = buildCatalogIndex()

func buildCatalogIndex() (map[string]struct{}, []string) {
	index := make(map[string]struct{}, len(catalog))
	var patterns []string
	for _, preset := range catalog {
		if !preset.Enabled || preset.DisplayOnly {
			continue
		}
		if preset.IsPattern() {
			patterns = append(patterns, preset.URL)
			continue
		}
		index[preset.URL] = struct{}{}
	}
	return index, patterns
}

// CatalogMatch reports whether clientID is admitted by an enabled catalog
// entry, and by which kind of entry. The AdmitReason is meaningful only
// when the bool is true.
//
// Exact entries compare case-sensitively with no normalization, per
// draft-02 §3's simple-string-comparison rule: the presented client_id must
// match the published document URL byte for byte. Pattern entries apply the
// restricted wildcard rules in pattern.go, which never widen the host.
//
// The two are reported separately rather than collapsed to a single
// "catalog" answer because the split is the only signal showing whether a
// wildcard entry is doing any work — see AdmitReason.
func CatalogMatch(clientID string) (AdmitReason, bool) {
	if _, ok := catalogURLs[clientID]; ok {
		return AdmitCatalogExact, true
	}
	for _, pattern := range catalogPatterns {
		if matchesPattern(pattern, clientID) {
			return AdmitCatalogPattern, true
		}
	}
	return "", false
}

// CatalogPreset resolves clientID to the enabled entry that admits it, exact
// URL first, then wildcard patterns in declaration order. It lets callers
// outside admission name a client by vendor rather than by the id it
// presented — a vendor minting one document per MCP server has no literal id
// to write a policy about.
func CatalogPreset(clientID string) (Preset, bool) {
	var pattern *Preset
	for i := range catalog {
		preset := catalog[i]
		if !preset.Enabled || preset.DisplayOnly {
			continue
		}
		if preset.IsPattern() {
			if pattern == nil && matchesPattern(preset.URL, clientID) {
				pattern = &catalog[i]
			}
			continue
		}
		if preset.URL == clientID {
			return preset, true
		}
	}
	if pattern != nil {
		return *pattern, true
	}
	return Preset{VendorKey: "", DisplayName: "", URL: "", DisplayOnly: false, Enabled: false}, false
}

// Catalog returns a copy of the full preset list, enabled and disabled
// alike, for the listPresets management endpoint. Returning disabled
// entries is deliberate — an operator asking "does Gram know about this
// vendor?" deserves a different answer from "no such vendor".
func Catalog() []Preset {
	return slices.Clone(catalog)
}
