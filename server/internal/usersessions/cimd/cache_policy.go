package cimd

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultCacheTTL applies when the document response carries no usable
	// freshness header. Draft -02 §5.2 lets the server pick; an hour keeps a
	// rotated document from lingering while still collapsing the repeated
	// authorize legs a single client makes in a session.
	defaultCacheTTL = time.Hour

	// minCacheTTL and maxCacheTTL bound whatever the upstream asks for. The
	// floor stops a hostile or misconfigured host from forcing a fetch on
	// every authorize (the request is unauthenticated, so the fetch is
	// attacker-triggerable); the ceiling stops it from pinning a document —
	// and therefore a redirect_uris set — indefinitely. -02 §5.2 explicitly
	// permits both bounds, and the 24h ceiling matches the MCP
	// specification's recommended maximum.
	minCacheTTL = 5 * time.Minute
	maxCacheTTL = 24 * time.Hour

	// maxETagLength caps the validator persisted in
	// client_id_metadata_etag and echoed back in If-None-Match. The value is
	// chosen by the document host, and net/http accepts response headers up
	// to MaxResponseHeaderBytes (1 MB by default), so an unbounded ETag is a
	// write amplification primitive against a TEXT column. Real validators
	// are a hash or a version stamp; anything longer is dropped and the next
	// refresh is unconditional.
	maxETagLength = 256
)

// cacheTTL derives the lifetime of a freshly fetched document from its
// response headers, following RFC 9111 §4.2: the granted freshness lifetime
// (s-maxage, then max-age, then Expires measured against Date) less the age
// the response already carries, clamped to [minCacheTTL, maxCacheTTL].
//
// no-store / no-cache are deliberately NOT honoured. A Client ID Metadata
// Document is public by definition — -02 §4.1 bans every secret from it — so
// a directive that forbids caching carries no confidentiality meaning here
// and would only let a host defeat the floor that keeps this endpoint from
// being an amplifier. This is a conscious RFC 9111 deviation of the kind
// -02 §5.2 permits.
//
// now is the reference instant the caller will also use to derive the
// absolute expiry, so a slow header parse cannot shift the two apart.
func cacheTTL(header http.Header, now time.Time) time.Duration {
	lifetime, ok := freshnessLifetime(header, now)
	if !ok {
		return defaultCacheTTL
	}

	// RFC 9111 §4.2.3: what is left of a lifetime is that lifetime minus the
	// age the response already carries. Skipping this would cache a
	// CDN-served document far longer than its origin allowed — a document
	// answered with max-age=86400 and Age: 86000 has minutes of freshness
	// left, not a day — which is exactly how a rotated redirect_uris set
	// would go unnoticed.
	return clampCacheTTL(lifetime - responseAge(header, now))
}

// responseAge is RFC 9111 §4.2.3's current_age: the larger of the Age header
// and the apparent age the Date header implies. Taking the larger of the two
// matters because they fail in different ways — an intermediary can replay a
// stale body without adding Age, and an origin can send an Age larger than
// its own Date suggests — and trusting only Age would hand a full lifetime to
// the first case.
//
// An absent or malformed Age reads as zero, and a Date in the future (a
// skewed origin clock) yields a negative apparent age that never wins the
// comparison, so the result is never negative.
func responseAge(header http.Header, now time.Time) time.Duration {
	age, _ := deltaSeconds(header.Get("Age"))
	if date, err := http.ParseTime(header.Get("Date")); err == nil {
		age = max(age, now.Sub(date))
	}
	return age
}

// freshnessLifetime is the lifetime the origin granted the response, before
// its current age is deducted.
func freshnessLifetime(header http.Header, now time.Time) (time.Duration, bool) {
	// RFC 9111 §5.2.2.10: s-maxage overrides max-age for a shared cache, and
	// this AS is one — a document fetched for one authorization is served to
	// every tenant that presents the same client_id.
	if sMaxAge, ok := cacheControlDelta(header, "s-maxage"); ok {
		return sMaxAge, true
	}
	if maxAge, ok := cacheControlDelta(header, "max-age"); ok {
		return maxAge, true
	}

	// http.ParseTime accepts the three formats RFC 9110 §5.6.7 requires.
	// An unparseable Expires (including the common "Expires: 0" spelling
	// meaning "already stale") falls through to the default rather than to
	// zero: the floor would clamp a zero TTL back up anyway, and treating a
	// malformed header as "no opinion" is the more predictable rule.
	expires, err := http.ParseTime(header.Get("Expires"))
	if err != nil {
		return 0, false
	}
	// RFC 9111 §4.2.1 measures Expires against the response's own Date, which
	// makes the result immune to a skewed origin clock. Falling back to the
	// local clock when Date is absent or unparseable is the best available
	// approximation.
	reference := now
	if date, err := http.ParseTime(header.Get("Date")); err == nil {
		reference = date
	}
	return expires.Sub(reference), true
}

func clampCacheTTL(ttl time.Duration) time.Duration {
	return min(max(ttl, minCacheTTL), maxCacheTTL)
}

// cacheControlDelta extracts a delta-seconds directive from every
// Cache-Control header on the response. RFC 9110 §5.3 lets a field repeat
// across lines, so Values is read rather than Get, which would see only the
// first line.
//
// A malformed or overflowing value reports not-found rather than a zero
// duration, so the caller falls through to the next source of freshness
// instead of pinning the row at the floor.
func cacheControlDelta(header http.Header, directive string) (time.Duration, bool) {
	for _, value := range header.Values("Cache-Control") {
		for candidate := range strings.SplitSeq(value, ",") {
			name, arg, found := strings.Cut(strings.TrimSpace(candidate), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(name), directive) {
				continue
			}
			if seconds, ok := deltaSeconds(arg); ok {
				return seconds, true
			}
		}
	}
	return 0, false
}

// deltaSeconds parses an RFC 9111 §1.2.2 delta-seconds value into a duration.
// It reports not-found for anything negative, unparseable, or large enough to
// overflow: the value is attacker-supplied and time.Duration is nanoseconds,
// so an unguarded multiplication would wrap into a negative duration that the
// clamp would silently read as "expire immediately".
func deltaSeconds(raw string) (time.Duration, bool) {
	// RFC 9110 §5.6.6 allows a directive argument to be sent as a
	// quoted-string even when the grammar calls for a token.
	seconds, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(raw), `"`), 10, 64)
	if err != nil || seconds < 0 || seconds > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// sanitizeETag returns the entity tag to persist and replay in If-None-Match,
// or "" when the response carries nothing usable. The value is stored and
// replayed verbatim (quotes and any W/ prefix included): If-None-Match is
// compared with RFC 9110 §13.1.2 weak comparison, so a weak validator is
// legitimate here and rewriting it would break revalidation against hosts
// that emit one.
//
// Anything that could corrupt the outbound request is dropped instead of
// escaped — a header value is not a place to be clever with attacker-chosen
// bytes, and dropping it costs only an unconditional refresh.
func sanitizeETag(raw string) string {
	etag := strings.TrimSpace(raw)
	if etag == "" || len(etag) > maxETagLength {
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
	// rotated redirect_uris set could never propagate. A list of tags is
	// rejected for the same reason — If-None-Match is a request for one
	// specific stored document, not for whichever of several the host likes.
	opaque, _ := strings.CutPrefix(etag, "W/")
	if len(opaque) < 2 || !strings.HasPrefix(opaque, `"`) || !strings.HasSuffix(opaque, `"`) {
		return ""
	}
	// etagc excludes DQUOTE, so an interior quote means the value is a list
	// or is otherwise malformed.
	if strings.Contains(opaque[1:len(opaque)-1], `"`) {
		return ""
	}
	return etag
}
