package mcpversions

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Published MCP protocol revisions, oldest first. Sourced from the
// specification's revision list (https://modelcontextprotocol.io/specification/versioning).
//
// These are NOT a statement of what Gram implements or accepts; the Served*
// constants below carry the revision each Gram surface answers with. They exist
// so telemetry can distinguish "a revision we know about" from "something else
// entirely".
const (
	Version20241105 = "2024-11-05"
	Version20250326 = "2025-03-26"
	Version20250618 = "2025-06-18"
	Version20251125 = "2025-11-25"
	Version20260728 = "2026-07-28"
)

// The protocol revision each Gram surface that terminates an MCP session
// answers with. Both are answered unconditionally, without regard to what the
// client requested — which is precisely why requested and negotiated are
// tracked as separate telemetry attributes rather than collapsed into one.
//
// They are split per surface deliberately, despite currently holding the same
// value. The surfaces face different client populations, so raising one is not
// the same decision as raising the other, and a supported-version ceiling is
// expected to move them on different schedules.
//
// The remote MCP proxy has no entry here by design: it never answers a
// version, it relays whatever the client and the upstream negotiate between
// themselves. Gram's outbound remote-URL verification probe is also absent —
// that is Gram acting as a client, so the version it sends is a requested one
// rather than a served one, and it lives with the probe.
const (
	// ServedHostedToolset is answered on /mcp/{slug} and on the
	// toolset-backed /x/mcp/{slug}, which share a handler. This surface faces
	// arbitrary third-party MCP clients, so changing it is a compatibility
	// event with external blast radius.
	ServedHostedToolset = Version20250326

	// ServedPlatformToolset is answered on /platform/mcp/{slug}, which accepts
	// only the assistant token — user OAuth, API keys, and chat sessions are
	// deliberately not honored there. That closed, first-party client set
	// means this can move without third-party exposure.
	ServedPlatformToolset = Version20250326
)

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

// Handshakeless reports whether v is a recognized revision that carries its
// protocol version on every request instead of agreeing one at `initialize`.
//
// 2026-07-28 removed the handshake, which is what makes this distinction worth
// drawing rather than simply comparing revisions. A client on a handshake-based
// revision learns what a server speaks by being told at `initialize`, so a
// surface answering an older revision than the client asked for is understood
// and the client adapts. A client on a handshake-less revision is never told
// anything: it declares a version per request and validates the reply against
// it. Serving one of those a response shaped for an older revision cannot be
// detected by the client, so the surface has to say so out loud — see
// [Serves].
func Handshakeless(v string) bool {
	idx := slices.Index(all, v)
	return idx >= 0 && idx >= slices.Index(all, Version20260728)
}

// Serves reports whether a surface answering the served revision can honour a
// request declaring the requested one.
//
// An empty or unrecognized request version is served: those are clients that
// either predate the header or are not speaking a revision this package knows,
// and both are better handled by the surface's existing behaviour than by a
// rejection. A handshake-less request for anything other than exactly the
// served revision is not, because such a client has no other way to discover
// the mismatch.
func Serves(served, requested string) bool {
	if requested == "" || !Known(requested) || requested == served {
		return true
	}

	return !Handshakeless(requested)
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
