package admission

import "slices"

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
var catalog = []Preset{
	// Verified 2026-07. Claude Code selects CIMD when the AS advertises
	// client_id_metadata_document_supported AND "none" among its
	// token_endpoint_auth_methods_supported.
	{
		VendorKey:   "anthropic",
		DisplayName: "Anthropic (Claude Code)",
		URL:         "https://claude.ai/oauth/claude-code-client-metadata",
		DisplayOnly: false,
		Enabled:     true,
	},
	{
		VendorKey:   "anthropic",
		DisplayName: "Anthropic (Claude)",
		URL:         "https://claude.ai/oauth/mcp-oauth-client-metadata",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07. Insiders ships a distinct document; both are
	// literal constants in the VS Code source.
	{
		VendorKey:   "microsoft",
		DisplayName: "Visual Studio Code",
		URL:         "https://vscode.dev/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},
	{
		VendorKey:   "microsoft",
		DisplayName: "Visual Studio Code (Insiders)",
		URL:         "https://insiders.vscode.dev/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07.
	{
		VendorKey:   "zed",
		DisplayName: "Zed",
		URL:         "https://zed.dev/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07. CIMD shipped in goose v1.32.0 (2026-04-23).
	{
		VendorKey:   "block",
		DisplayName: "Goose",
		URL:         "https://goose-docs.ai/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07, re-verified 2026-08-19 (all four entries fetched
	// live). OpenAI mints CIMD documents per connector and per Codex target
	// server, so those namespaces are unbounded and only patterns can admit
	// them — see pattern.go.
	//
	// chatgpt.com is a TEMPLATE endpoint: any id in a wildcard position
	// returns HTTP 200 with a valid self-referential document, so a
	// successful fetch proves nothing about an id being real. The patterns
	// stay safe because the id is never reflected into consent-visible
	// metadata — client_name, client_uri, and logo_uri are constant per
	// product — and the per-id redirect_uris are loopback-only, so minting
	// a document at a chosen id gains an attacker nothing over presenting
	// the vendor's real client_id.
	{
		// One document per ChatGPT connector, {id} derived from the MCP
		// server origin (per OpenAI's Apps SDK docs and comments in the
		// Codex CLI source: SHAKE-256 over the origin) — deterministic per
		// server, unbounded across servers.
		VendorKey:   "openai",
		DisplayName: "ChatGPT (connectors)",
		URL:         "https://chatgpt.com/oauth/*/client.json",
		DisplayOnly: false,
		Enabled:     true,
	},
	{
		// The connector platform's stable shared document. NOT DisplayOnly:
		// the connector wildcard requires exactly one path segment between
		// /oauth/ and /client.json, and this URL has none, so nothing else
		// admits it. It is a rule in its own right.
		VendorKey:   "openai",
		DisplayName: "ChatGPT",
		URL:         "https://chatgpt.com/oauth/client.json",
		DisplayOnly: false,
		Enabled:     true,
	},
	{
		// Codex CLI ≥0.148.0 (first stable release 2026-08-18, openai/codex
		// PR #38089; earlier versions used dynamic client registration and
		// never presented a CIMD client_id) mints one document per MCP
		// server: {id} is the base64url-no-pad encoding of the first 9
		// bytes of SHA-256 of the full server URL, computed client-side.
		// Deterministic per server URL, not per install — every install
		// pointed at the same server presents the same client_id — but the
		// server URL space is unbounded, so only a pattern can admit it.
		VendorKey:   "openai",
		DisplayName: "Codex CLI",
		URL:         "https://chatgpt.com/oauth/codex/*/client.json",
		DisplayOnly: false,
		Enabled:     true,
	},
	{
		// The stable shared Codex document. No released Codex version has
		// ever presented it — the 2026-07 entry was assembled from OpenAI's
		// docs, not observed traffic — but OpenAI's docs as of 2026-08
		// state Codex will switch to it, once client support ships, for
		// authorization servers that advertise
		// authorization_response_iss_parameter_supported (RFC 9207).
		// DisplayOnly because the connector wildcard (one segment, here
		// "codex") already admits it; when Codex flips to this document no
		// catalog change is needed.
		VendorKey:   "openai",
		DisplayName: "Codex CLI (stable document)",
		URL:         "https://chatgpt.com/oauth/codex/client.json",
		DisplayOnly: true,
		Enabled:     true,
	},

	// Verified 2026-07. Notion publishes two equally-valid documents on two
	// origins, each self-consistent with its own client_uri and
	// redirect_uris. Seeding only one would reject half of Notion's
	// traffic. The apex (notion.so, no www) 301s to the www document, whose
	// client_id is the www form, so the apex URL is not itself a valid
	// client_id and is deliberately absent.
	{
		VendorKey:   "notion",
		DisplayName: "Notion",
		URL:         "https://www.notion.so/oauth/mcp-client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},
	{
		VendorKey:   "notion",
		DisplayName: "Notion (app.notion.com)",
		URL:         "https://app.notion.com/oauth/mcp-client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07.
	{
		VendorKey:   "mcpjam",
		DisplayName: "MCPJam Inspector",
		URL:         "https://www.mcpjam.com/.well-known/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07. CIMD shipped in droid 0.148.0 (2026-06-15).
	{
		VendorKey:   "factory",
		DisplayName: "Factory Droid",
		URL:         "https://api.factory.ai/mcp/oauth-client",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-07.
	{
		VendorKey:   "stacklok",
		DisplayName: "ToolHive",
		URL:         "https://toolhive.dev/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-08-21. Hermes Agent is served from GitHub Pages
	// (nousresearch.github.io). Exact-match admission keeps this scoped to
	// the single document path; the shared github.io apex is not a widening
	// concern.
	{
		VendorKey:   "nousresearch",
		DisplayName: "Hermes Agent",
		URL:         "https://nousresearch.github.io/hermes-agent/docs/oauth/client-metadata.json",
		DisplayOnly: false,
		Enabled:     true,
	},

	// Verified 2026-08-26. The apex form (skydive.com, no www) 301s to the
	// www document, whose client_id is the www form, so the apex URL is not
	// itself a valid client_id and is deliberately absent.
	{
		VendorKey:   "skydive",
		DisplayName: "Skydive",
		URL:         "https://www.skydive.com/api/v1/external-oauth/client-metadata",
		DisplayOnly: false,
		Enabled:     true,
	},
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

// Catalog returns a copy of the full preset list, enabled and disabled
// alike, for the listPresets management endpoint. Returning disabled
// entries is deliberate — an operator asking "does Gram know about this
// vendor?" deserves a different answer from "no such vendor".
func Catalog() []Preset {
	return slices.Clone(catalog)
}
