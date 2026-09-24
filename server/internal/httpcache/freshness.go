package httpcache

import (
	"net/http"
	"time"
)

// FreshnessPolicy bounds the freshness lifetime a client-side cache grants a
// fetched response. Consumers fetching remote documents (Client ID Metadata
// Documents, JWK Sets) each pick their own bounds; the header parsing they
// share lives here.
type FreshnessPolicy struct {
	// Default applies when the response carries no usable freshness header.
	Default time.Duration

	// Min is the floor on whatever the upstream asks for. Consumers on
	// attacker-triggerable surfaces rely on it to keep a hostile or
	// misconfigured host from forcing a refetch on every request.
	Min time.Duration

	// Max is the ceiling on whatever the upstream asks for, so a host cannot
	// pin a stale document indefinitely.
	Max time.Duration
}

// TTL derives the lifetime of a freshly fetched response from its headers,
// following RFC 9111 §4.2: the granted freshness lifetime (s-maxage, then
// max-age, then Expires measured against Date) less the age the response
// already carries, clamped to [Min, Max].
//
// no-store / no-cache are deliberately NOT honoured. Every consumer of this
// policy caches documents that are public by definition, so a directive that
// forbids caching carries no confidentiality meaning and would only let a
// host defeat the Min floor that keeps attacker-triggerable endpoints from
// becoming outbound fetch amplifiers. This is a conscious RFC 9111 deviation.
//
// now is the local reference instant, read when the response carries no
// usable Date: the Expires fallback and the apparent age are both measured
// against it then.
func (p FreshnessPolicy) TTL(header http.Header, now time.Time) time.Duration {
	lifetime, ok := freshnessLifetime(header, now)
	if !ok {
		return p.Default
	}

	// RFC 9111 §4.2.3: what is left of a lifetime is that lifetime minus the
	// age the response already carries. Skipping this would cache a
	// CDN-served document far longer than its origin allowed — a document
	// answered with max-age=86400 and Age: 86000 has minutes of freshness
	// left, not a day.
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
		return p.Min
	}
	return min(max(lifetime-age, p.Min), p.Max)
}

// freshnessLifetime is the lifetime the origin granted the response, before
// its current age is deducted. It spans headers by design — Cache-Control's
// s-maxage and max-age directives, then Expires measured against Date — which
// is why it lives with the policy rather than in a per-header file.
func freshnessLifetime(header http.Header, now time.Time) (time.Duration, bool) {
	// RFC 9111 §5.2.2.10: s-maxage overrides max-age for a shared cache, and
	// every consumer of this policy is one — a document fetched once is
	// served to every tenant that triggers the same lookup.
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
