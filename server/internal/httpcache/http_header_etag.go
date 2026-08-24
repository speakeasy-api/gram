package httpcache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SanitizeETag returns the entity tag to persist and replay in If-None-Match,
// or "" when the value is unusable. The value is stored and replayed verbatim
// (quotes and any W/ prefix included): If-None-Match is compared with RFC
// 9110 §13.1.2 weak comparison, so a weak validator is legitimate here and
// rewriting it would break revalidation against hosts that emit one.
//
// maxLength caps the persisted validator. The value is chosen by the document
// host, and net/http accepts response headers up to MaxResponseHeaderBytes
// (1 MB by default), so an unbounded ETag is a write amplification primitive
// against whatever stores it. Real validators are a hash or a version stamp;
// anything longer is dropped and the next refresh is unconditional.
//
// Anything that could corrupt the outbound request is dropped instead of
// escaped — a header value is not a place to be clever with attacker-chosen
// bytes, and dropping it costs only an unconditional refresh.
func SanitizeETag(raw string, maxLength int) string {
	etag := strings.TrimSpace(raw)
	if etag == "" || len(etag) > maxLength {
		return ""
	}
	// Reject anything outside printable US-ASCII. This covers CR and LF
	// (request splitting) and also keeps the stored value inside what
	// net/http will agree to send: a header value it considers invalid makes
	// every later conditional request fail outright.
	for _, r := range etag {
		if r < ' ' || r > '~' {
			return ""
		}
	}

	// RFC 9110 §8.8.3: entity-tag = [ "W/" ] DQUOTE *etagc DQUOTE. Enforcing
	// the grammar rather than storing any printable string is what keeps the
	// wildcard out. "ETag: *" is not a valid response header, but a host that
	// sends one would have it replayed as "If-None-Match: *", which §13.1.2
	// defines as matching whenever the origin has any representation at all:
	// every revalidation would then answer 304 and extend the cache, so a
	// rotated document could never propagate. A list of tags is rejected for
	// the same reason — If-None-Match is a request for one specific stored
	// document, not for whichever of several the host likes.
	opaque, _ := strings.CutPrefix(etag, "W/")
	if len(opaque) < 2 || !strings.HasPrefix(opaque, `"`) || !strings.HasSuffix(opaque, `"`) {
		return ""
	}
	// etagc is %x21 / %x23-7E / obs-text, so within the printable ASCII this
	// function already requires it admits everything except SP and DQUOTE. An
	// interior quote means the value is a list or is otherwise malformed, and
	// a space cannot appear in a well-formed tag at all. Dropping such a
	// validator costs an unconditional refresh, which is the safe direction.
	if strings.ContainsAny(opaque[1:len(opaque)-1], " \"") {
		return ""
	}
	return etag
}

// strongETag returns a strong ETag (RFC 9110 §8.8.3): a quoted hex SHA-256
// digest of body. Strong because it is computed from the exact bytes written,
// before any downstream compression. Host-derived URLs are part of body, so
// two hosts of the same resource naturally get distinct ETags without Vary.
func strongETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// ifNoneMatchSatisfied reports whether an If-None-Match header value matches
// etag, using the weak comparison RFC 9110 §13.1.2 mandates for If-None-Match
// (the W/ prefix is ignored on both sides). "*" matches any current
// representation.
func ifNoneMatchSatisfied(header, etag string) bool {
	header = strings.TrimSpace(header)
	switch header {
	case "":
		return false
	case "*":
		return true
	}

	want := strings.TrimPrefix(etag, "W/")
	for candidate := range strings.SplitSeq(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == want {
			return true
		}
	}
	return false
}
