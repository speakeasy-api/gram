package mcpversions

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Published MCP protocol revisions, oldest first. Sourced from the
// specification's revision list (https://modelcontextprotocol.io/specification/versioning).
//
// These are NOT a statement of what Gram implements or accepts; the Supported*
// functions below carry the revisions each Gram surface actually negotiates
// and serves. They exist so telemetry can distinguish "a revision we know
// about" from "something else entirely".
const (
	Version20241105 = "2024-11-05"
	Version20250326 = "2025-03-26"
	Version20250618 = "2025-06-18"
	Version20251125 = "2025-11-25"
	Version20260728 = "2026-07-28"
)

// DefaultInEffect is the revision put in effect when no usable version can be
// taken from the request: a request that names no version at all, and a
// per-request declaration outside the surface's supported set. The 2026-07-28
// specification sanctions exactly this value for the first case — a server
// supporting pre-2025-06-18 clients MAY treat a request that omits the
// version header as 2025-03-26 — and the second case deliberately reuses it:
// defaulting downward over-serves rather than wrongly rejecting, which is the
// safe direction for both cohorts.
const DefaultInEffect = Version20250326

// The protocol revisions each Gram surface that terminates an MCP session
// supports, oldest first. A revision in a surface's set is echoed at
// `initialize` and governs per-request behavior when declared; anything
// outside the set is answered with the newest member, per the spec's rule that
// a server must respond with a version it supports and should pick its latest.
//
// The sets are split per surface deliberately, despite currently holding the
// same values. The surfaces face different client populations, so raising one
// is not the same decision as raising the other, and the ceilings are expected
// to move on different schedules. The current ceiling is Version20251125;
// advertising Version20260728 is its own project with its own preconditions.
// The Version20241105 floor is evidence-based — clients on that revision still
// make tool calls — and claims support on the Streamable HTTP transport only,
// not the HTTP+SSE transport that revision also defined.
//
// The remote MCP proxy has no entry here by design: it never answers a
// version, it relays whatever the client and the upstream negotiate between
// themselves. Gram's outbound remote-URL verification probe is also absent —
// that is Gram acting as a client, so the version it sends is a requested one
// rather than a served one, and it lives with the probe.
var (
	supportedHostedToolset   = []string{Version20241105, Version20250326, Version20250618, Version20251125}
	supportedPlatformToolset = []string{Version20241105, Version20250326, Version20250618, Version20251125}
)

// SupportedHostedToolset returns the revisions supported on /mcp/{slug} and on
// the toolset-backed /x/mcp/{slug}, which share a handler, oldest first. This
// surface faces arbitrary third-party MCP clients, so changing the set is a
// compatibility event with external blast radius.
func SupportedHostedToolset() []string {
	return slices.Clone(supportedHostedToolset)
}

// SupportedPlatformToolset returns the revisions supported on
// /platform/mcp/{toolsetSlug}, oldest first. That surface accepts only the
// assistant token — user OAuth, API keys, and chat sessions are deliberately
// not honored there — so its closed, first-party client set can move without
// third-party exposure.
func SupportedPlatformToolset() []string {
	return slices.Clone(supportedPlatformToolset)
}

// Negotiate applies the MCP version-negotiation rule to an `initialize`
// request: a supported requested revision is echoed, an absent one resolves to
// [DefaultInEffect], and anything else — unknown, unrecognized, or known
// but unsupported — is answered with the newest supported revision. The
// absent case deliberately does not fall through to the ceiling: a client
// that omitted the field entirely is the cohort likeliest to break on a new
// revision, and the spec's omitted-version rule points at 2025-03-26.
//
// requested may be raw client input; it is bounded by [Sanitize] first.
// supported must be non-empty and ordered oldest first, as the Supported*
// functions return.
func Negotiate(requested string, supported []string) string {
	requested = Sanitize(requested)
	switch {
	case requested == "":
		return DefaultInEffect
	case slices.Contains(supported, requested):
		return requested
	default:
		return supported[len(supported)-1]
	}
}

// Resolution is the protocol revision resolved for one MCP request. It keeps
// the two version semantics a request carries distinguishable: what the client
// said, for telemetry, and what governs the request, for behavior. Confusing
// the two silently corrupts one or the other — feeding InEffect to a census
// metric fabricates data points for clients that declared nothing, and
// branching behavior on Declared trusts a value outside the supported set.
type Resolution struct {
	// Declared is the sanitized revision the request declared — the
	// MCP-Protocol-Version header, falling back to the 2026-07-28 per-request
	// `_meta` key. It may be empty (every legacy `initialize`, and every
	// request from a pre-2025-06-18 client) or a revision outside the
	// supported set. Telemetry consumers own this half: metric dimensions
	// clamp it themselves so absent and unknown declarations stay countable.
	Declared string

	// InEffect is the revision governing the request: Declared when the
	// surface supports it, otherwise [DefaultInEffect]. Never empty.
	// Version-conditional behavior branches on this value and nothing else.
	//
	// For an `initialize` request the entry-time value is provisional — a
	// conforming handshake declares no version, so it starts at the default —
	// and the initialize handler overwrites it with the negotiated answer
	// once computed, superseding whatever a nonconforming header may have
	// resolved to. That write-back is the one sanctioned mutation after
	// [Resolve].
	InEffect string
}

// Resolve computes the [Resolution] for a request that declared declared (raw
// client input; bounded by [Sanitize] here) against a surface's supported
// set. A declared revision outside the set resolves to [DefaultInEffect],
// deliberately over-serving downward instead of rejecting; replacing that arm
// with the spec's UnsupportedProtocolVersionError (-32022) is separate,
// planned work, so callers must not treat the fallback as a permanent
// contract.
func Resolve(declared string, supported []string) Resolution {
	declared = Sanitize(declared)
	inEffect := DefaultInEffect
	if slices.Contains(supported, declared) {
		inEffect = declared
	}

	return Resolution{Declared: declared, InEffect: inEffect}
}

// HTTPHeader is the header MCP clients stamp on every request once the
// protocol version is established. Under the handshake-based revisions it
// carries the version negotiated at `initialize`; under 2026-07-28 it mirrors
// the per-request `io.modelcontextprotocol/protocolVersion` `_meta` key.
// Either way it is the version in effect for the request carrying it.
//
// It is absent from the `initialize` request itself, since nothing is
// negotiated yet at that point.
const HTTPHeader = "MCP-Protocol-Version"

// Other and None are the two synthetic buckets [Clamp] emits, so that every
// point on a versioned metric carries the same dimensions and a breakdown by
// version accounts for all traffic rather than silently dropping the awkward
// cases. Neither can collide with a real revision, which is always a date; a
// client that literally sends "other" or "none" is unrecognized and buckets
// into Other like any other unknown value.
const (
	// Other collects every unrecognized version. Sustained traffic here means
	// either a new revision shipped and this package has gone stale, or a
	// client is sending garbage; both are worth an alert.
	Other = "other"

	// None marks a client that supplied no version at all. Distinct from Other
	// because the two imply different things about the client: Other is a
	// client naming a revision nobody here knows, None is a client that named
	// none, which on a handshake means it omitted a field the protocol
	// requires. That cohort is the likeliest to break under a version ceiling,
	// so it needs to be countable rather than absent.
	None = "none"
)

// maxRawLength bounds a client-supplied version before it becomes a span
// attribute. Every real revision identifier is exactly 10 characters; the
// allowance leaves room to see what a malformed sender actually sent without
// letting it write an unbounded string into telemetry.
const maxRawLength = 32

// all lists every recognized revision, oldest first. Order is part of the
// contract: it is the basis for any "is this revision older than X" comparison
// a version ceiling would need.
var all = []string{
	Version20241105,
	Version20250326,
	Version20250618,
	Version20251125,
	Version20260728,
}

// All returns the recognized protocol revisions, oldest first.
func All() []string {
	return slices.Clone(all)
}

// Known reports whether v is a protocol revision this package recognizes.
func Known(v string) bool {
	return slices.Contains(all, v)
}

// Clamp bounds a client-supplied version for use as a metric dimension: a
// recognized revision passes through, an absent one becomes [None], and
// anything else becomes [Other].
//
// The result is never empty, so callers record the dimension unconditionally.
// Omitting it instead would emit a differently-shaped series for the clients
// that supplied nothing, leaving that cohort countable only by subtracting the
// labelled series from a total that is not itself recorded.
//
// Span attributes want the opposite treatment and use [Sanitize]: on a trace an
// absent attribute already reads as absent, and a synthetic bucket would be
// noise.
func Clamp(v string) string {
	switch {
	case v == "":
		return None
	case Known(v):
		return v
	default:
		return Other
	}
}

// Sanitize bounds an untrusted, client-supplied version before it is recorded
// as a span attribute. The value is trimmed and length-capped, and is rejected
// outright if it carries anything beyond printable ASCII, so a hostile sender
// cannot write control bytes into telemetry. Unlike [Clamp] the
// result is not restricted to known revisions: the raw value is the diagnostic
// payload, and seeing what an unrecognized client actually sent is the point.
func Sanitize(v string) string {
	v = conv.TruncateString(strings.TrimSpace(v), maxRawLength)
	for _, r := range v {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}

	return v
}
