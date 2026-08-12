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
// now is the local reference instant, read when the response carries no
// usable Date: the Expires fallback and the apparent age are both measured
// against it then.
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
	//
	// The comparison stands in for a subtraction that could wrap: the
	// lifetime may be hugely negative (an Expires far before Date) and the
	// age saturates at MaxInt64, so lifetime-minus-age can fall past
	// MinInt64 and come out large and positive — the clamp would then read
	// two "this is ancient" signals as maximum freshness. Age is never
	// negative, so age >= lifetime is exactly "no freshness left", and
	// otherwise the difference is positive and cannot wrap.
	age := responseAge(header, now)
	if age >= lifetime {
		return minCacheTTL
	}
	return clampCacheTTL(lifetime - age)
}

// responseAge is RFC 9111 §4.2.3's current_age: the larger of the Age header
// and the apparent age the Date header implies. Taking the larger of the two
// matters because they fail in different ways — an intermediary can replay a
// stale body without adding Age, and an origin can send an Age larger than
// its own Date suggests — and trusting only Age would hand a full lifetime to
// the first case.
//
// It approximates rather than reproduces the RFC's current_age, which also
// adds the request delay and the time the response has since spent resident
// in the cache. Resident time is zero by construction here (the absolute
// expiry is derived from this the moment the fetch returns) and the request
// delay is bounded by fetchTimeout, so both are far below the five minute
// floor every result is clamped to.
//
// An absent or malformed Age reads as zero, and a Date in the future (a
// skewed origin clock) yields a negative apparent age that never wins the
// comparison, so the result is never negative.
func responseAge(header http.Header, now time.Time) time.Duration {
	age := headerAge(header)
	if date, err := http.ParseTime(header.Get("Date")); err == nil {
		age = max(age, now.Sub(date))
	}
	return age
}

// headerAge reads the Age header.
//
// An age too large to represent is treated as the opposite of an unparseable
// lifetime. A lifetime we cannot parse means the host expressed no opinion,
// so the default applies; an age we cannot parse because it is astronomically
// large is the host saying the response is ancient, so it saturates and the
// subtraction drives the TTL to the floor. Reading it as zero instead would
// grant a document the host called stale its full lifetime.
func headerAge(header http.Header) time.Duration {
	raw := header.Get("Age")
	if raw == "" {
		return 0
	}
	if age, ok := deltaSeconds(raw); ok {
		return age
	}
	if _, wellFormed := deltaSecondsDigits(raw); wellFormed {
		return time.Duration(math.MaxInt64)
	}
	return 0
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
		for _, candidate := range splitDirectives(value) {
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

// splitDirectives splits one Cache-Control field value on the commas that
// separate directives, leaving commas inside a quoted-string alone.
//
// Splitting naively would let a quoted argument masquerade as a directive:
// no-cache="Set-Cookie, max-age=99999" breaks into a fragment reading
// `max-age=99999"`, which is a lifetime the host never granted. Directives
// that take a quoted field-name list (no-cache, private) are exactly the ones
// where this arises, and the field value is chosen by the document host.
func splitDirectives(value string) []string {
	directives := make([]string, 0, strings.Count(value, ",")+1)
	quoted := false
	start := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			// RFC 9110 §5.6.4 quoted-pair: the next octet is literal, so it
			// can neither open nor close the string.
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				directives = append(directives, value[start:i])
				start = i + 1
			}
		}
	}
	// An unterminated quoted-string means the field value is malformed and
	// its directive boundaries are guesswork. Discarding it entirely keeps
	// the same rule the rest of this parser follows: what cannot be read as
	// the host meant it is treated as no opinion, not as a partial one.
	if quoted {
		return nil
	}
	return append(directives, value[start:])
}

// deltaSeconds parses an RFC 9111 §1.2.2 delta-seconds value into a duration.
// It reports not-found for anything negative, unparseable, or large enough to
// overflow: the value is attacker-supplied and time.Duration is nanoseconds,
// so an unguarded multiplication would wrap into a negative duration that the
// clamp would silently read as "expire immediately".
func deltaSeconds(raw string) (time.Duration, bool) {
	digits, ok := deltaSecondsDigits(raw)
	if !ok {
		return 0, false
	}

	// Guard the multiplication rather than the result: time.Duration is
	// nanoseconds, so a value past this bound overflows into a negative
	// duration that the clamp would silently read as "expire immediately".
	seconds, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || seconds > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// deltaSecondsDigits returns the digits of a well-formed delta-seconds value,
// reporting false for anything that is not one. The grammar is 1*DIGIT, so a
// sign is not part of it: strconv would happily read "+7200" and "-0", and
// honouring either would mean acting on a lifetime the host did not express.
//
// It reports true for digit strings too large to be a duration, which is why
// it is separate from deltaSeconds — the two callers want opposite fallbacks
// for an overflow.
func deltaSecondsDigits(raw string) (string, bool) {
	value := strings.TrimSpace(raw)

	// RFC 9110 §5.6.6 allows a directive argument to be sent as a
	// quoted-string even when the grammar calls for a token, but only as a
	// matched pair. Stripping stray quotes instead would accept a value the
	// upstream never expressed.
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}

	if value == "" {
		return "", false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return "", false
		}
	}
	return value, true
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
