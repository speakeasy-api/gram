package cimd

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// cacheTTLReference is the fixed instant Expires-based cases are measured
// against, so the arithmetic in the test is exact rather than racing the
// clock.
var cacheTTLReference = time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)

func headerWith(t *testing.T, pairs ...string) http.Header {
	t.Helper()

	require.Zero(t, len(pairs)%2, "pairs must be key/value")
	header := http.Header{}
	for i := 0; i < len(pairs); i += 2 {
		header.Add(pairs[i], pairs[i+1])
	}
	return header
}

func TestCacheTTL_MaxAgeHonoured(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", "public, max-age=7200"), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_MaxAgeBelowFloorClamped(t *testing.T) {
	t.Parallel()

	// The acceptance case from the issue: an upstream asking for 2 minutes
	// reads as the 5 minute floor, so an attacker-triggerable endpoint can
	// never be pushed into fetching on every authorize.
	ttl := cacheTTL(headerWith(t, "Cache-Control", "max-age=120"), cacheTTLReference)
	require.Equal(t, minCacheTTL, ttl)
}

func TestCacheTTL_MaxAgeAboveCeilingClamped(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", "max-age=31536000"), cacheTTLReference)
	require.Equal(t, maxCacheTTL, ttl)
}

func TestCacheTTL_MaxAgeZeroClampedToFloor(t *testing.T) {
	t.Parallel()

	// max-age=0 is a well-formed request to never cache. The floor applies
	// anyway: caching a public document is the point of this package, and
	// -02 §5.2 permits a lower bound.
	ttl := cacheTTL(headerWith(t, "Cache-Control", "max-age=0"), cacheTTLReference)
	require.Equal(t, minCacheTTL, ttl)
}

func TestCacheTTL_MaxAgeQuoted(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", `max-age="7200"`), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_MaxAgeAcrossRepeatedHeaders(t *testing.T) {
	t.Parallel()

	// RFC 9110 §5.3 lets a field repeat across lines; reading only the first
	// would miss this directive entirely.
	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "public",
		"Cache-Control", "max-age=7200",
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_MaxAgeCaseInsensitive(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", "Max-Age=7200"), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_CommaInsideQuotedDirectiveIsNotADirective(t *testing.T) {
	t.Parallel()

	// The quoted argument of no-cache must not be split into a fragment that
	// reads as a max-age the host never granted.
	ttl := cacheTTL(headerWith(t, "Cache-Control", `no-cache="Set-Cookie, max-age=99999"`), cacheTTLReference)
	require.Equal(t, defaultCacheTTL, ttl)
}

func TestCacheTTL_QuotedDirectiveDoesNotHideRealMaxAge(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", `private="Set-Cookie, Authorization", max-age=7200`), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_EscapedQuoteInsideDirectiveHandled(t *testing.T) {
	t.Parallel()

	// A quoted-pair escaping a quote must not be read as closing the string,
	// which would put the rest of the value back in directive position.
	ttl := cacheTTL(headerWith(t, "Cache-Control", `no-cache="a\", max-age=99999", max-age=7200`), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_UnmatchedQuoteFallsBackToDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, defaultCacheTTL, cacheTTL(headerWith(t, "Cache-Control", `max-age="7200`), cacheTTLReference))
	require.Equal(t, defaultCacheTTL, cacheTTL(headerWith(t, "Cache-Control", `max-age=7200"`), cacheTTLReference))
	require.Equal(t, defaultCacheTTL, cacheTTL(headerWith(t, "Cache-Control", `max-age=""7200""`), cacheTTLReference))
}

func TestCacheTTL_MaxAgeOverflowFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// A value past the int64 nanosecond range must not wrap into a negative
	// duration, which the clamp would silently read as "expire immediately".
	ttl := cacheTTL(headerWith(t, "Cache-Control", "max-age=99999999999999999999"), cacheTTLReference)
	require.Equal(t, defaultCacheTTL, ttl)
}

func TestCacheTTL_MaxAgeNegativeFallsBackToDefault(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", "max-age=-30"), cacheTTLReference)
	require.Equal(t, defaultCacheTTL, ttl)
}

func TestCacheTTL_MaxAgeMalformedFallsBackToDefault(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", "max-age=soon"), cacheTTLReference)
	require.Equal(t, defaultCacheTTL, ttl)
}

func TestCacheTTL_NoStoreIgnored(t *testing.T) {
	t.Parallel()

	// A deliberate RFC 9111 deviation: the document is public by definition,
	// so no-store carries no confidentiality meaning here and honouring it
	// would only defeat the floor.
	ttl := cacheTTL(headerWith(t, "Cache-Control", "no-store, no-cache, must-revalidate"), cacheTTLReference)
	require.Equal(t, defaultCacheTTL, ttl)
}

func TestCacheTTL_NoStoreDoesNotOverrideMaxAge(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t, "Cache-Control", "no-cache, max-age=7200"), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_SharedMaxAgePreferredOverMaxAge(t *testing.T) {
	t.Parallel()

	// RFC 9111 §5.2.2.10: this AS is a shared cache, so s-maxage wins.
	ttl := cacheTTL(headerWith(t, "Cache-Control", "s-maxage=7200, max-age=86400"), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_AgeDeductedFromMaxAge(t *testing.T) {
	t.Parallel()

	// The CDN case: a day of granted freshness that has already been sitting
	// in an intermediary for 22 hours leaves 2 hours, not 24.
	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "79200",
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_AgeExceedingLifetimeClampedToFloor(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "90000",
	), cacheTTLReference)
	require.Equal(t, minCacheTTL, ttl)
}

func TestCacheTTL_ApparentAgeFromDateDeducted(t *testing.T) {
	t.Parallel()

	// An intermediary replaying a stale body without adding an Age header:
	// the Date it kept is what reveals how much freshness is really left.
	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Date", cacheTTLReference.Add(-22*time.Hour).Format(http.TimeFormat),
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_LargerOfAgeAndApparentAgeWins(t *testing.T) {
	t.Parallel()

	// Age says 22 hours while Date implies only one, so Age wins.
	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "79200",
		"Date", cacheTTLReference.Add(-time.Hour).Format(http.TimeFormat),
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)

	// Reversed: Date implies 22 hours while Age claims one.
	ttl = cacheTTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "3600",
		"Date", cacheTTLReference.Add(-22*time.Hour).Format(http.TimeFormat),
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_FutureDateDoesNotExtendLifetime(t *testing.T) {
	t.Parallel()

	// A skewed origin clock implies a negative apparent age, which must not
	// be added back onto the lifetime.
	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=7200",
		"Date", cacheTTLReference.Add(time.Hour).Format(http.TimeFormat),
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_MalformedAgeIgnored(t *testing.T) {
	t.Parallel()

	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=7200",
		"Age", "ancient",
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_ExpiresMeasuredAgainstDate(t *testing.T) {
	t.Parallel()

	// An origin whose clock runs two hours fast still grants exactly the two
	// hours it meant to, because Expires is read relative to its own Date.
	originNow := cacheTTLReference.Add(2 * time.Hour)
	ttl := cacheTTL(headerWith(t,
		"Date", originNow.Format(http.TimeFormat),
		"Expires", originNow.Add(2*time.Hour).Format(http.TimeFormat),
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_ExpiresHonouredWhenNoMaxAge(t *testing.T) {
	t.Parallel()

	expires := cacheTTLReference.Add(2 * time.Hour).Format(http.TimeFormat)
	ttl := cacheTTL(headerWith(t, "Expires", expires), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_MaxAgePreferredOverExpires(t *testing.T) {
	t.Parallel()

	expires := cacheTTLReference.Add(20 * time.Hour).Format(http.TimeFormat)
	ttl := cacheTTL(headerWith(t,
		"Cache-Control", "max-age=7200",
		"Expires", expires,
	), cacheTTLReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestCacheTTL_ExpiresInPastClampedToFloor(t *testing.T) {
	t.Parallel()

	expires := cacheTTLReference.Add(-time.Hour).Format(http.TimeFormat)
	ttl := cacheTTL(headerWith(t, "Expires", expires), cacheTTLReference)
	require.Equal(t, minCacheTTL, ttl)
}

func TestCacheTTL_ExpiresZeroFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// "Expires: 0" is the common spelling of "already stale" and is not a
	// valid HTTP-date, so it reads as no opinion rather than as the floor.
	ttl := cacheTTL(headerWith(t, "Expires", "0"), cacheTTLReference)
	require.Equal(t, defaultCacheTTL, ttl)
}

func TestCacheTTL_NoHeadersUsesDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, defaultCacheTTL, cacheTTL(http.Header{}, cacheTTLReference))
}

func TestSanitizeETag_StrongAndWeakPreservedVerbatim(t *testing.T) {
	t.Parallel()

	// The validator is replayed byte for byte: If-None-Match uses weak
	// comparison, so rewriting a W/ prefix would break revalidation against
	// hosts that emit one.
	require.Equal(t, `"abc123"`, sanitizeETag(`"abc123"`))
	require.Equal(t, `W/"abc123"`, sanitizeETag(`W/"abc123"`))
	require.Equal(t, `"abc123"`, sanitizeETag(`  "abc123"  `))
}

func TestSanitizeETag_EmptyDropped(t *testing.T) {
	t.Parallel()

	require.Empty(t, sanitizeETag(""))
	require.Empty(t, sanitizeETag("   "))
}

func TestSanitizeETag_OversizedDropped(t *testing.T) {
	t.Parallel()

	atLimit := `"` + strings.Repeat("a", maxETagLength-2) + `"`
	require.Len(t, atLimit, maxETagLength)
	require.Equal(t, atLimit, sanitizeETag(atLimit))
	require.Empty(t, sanitizeETag(`"`+strings.Repeat("a", maxETagLength-1)+`"`))
}

func TestSanitizeETag_WildcardDropped(t *testing.T) {
	t.Parallel()

	// "ETag: *" is not a valid response header, and replaying it as
	// "If-None-Match: *" would match whenever the host has any
	// representation at all, so every revalidation would answer 304 and the
	// cached document could never be superseded.
	require.Empty(t, sanitizeETag("*"))
	require.Empty(t, sanitizeETag("W/*"))
}

func TestSanitizeETag_UnquotedDropped(t *testing.T) {
	t.Parallel()

	require.Empty(t, sanitizeETag("abc123"))
	require.Empty(t, sanitizeETag(`"abc123`))
	require.Empty(t, sanitizeETag(`abc123"`))
	require.Empty(t, sanitizeETag(`"`))
}

func TestSanitizeETag_InteriorSpaceDropped(t *testing.T) {
	t.Parallel()

	// etagc admits neither SP nor DQUOTE, so a spaced tag is malformed even
	// though a space is legal elsewhere in a header value.
	require.Empty(t, sanitizeETag(`"abc def"`))
	require.Empty(t, sanitizeETag(`W/"abc def"`))
}

func TestSanitizeETag_ListDropped(t *testing.T) {
	t.Parallel()

	// If-None-Match here asks about one specific stored document, so a list
	// of candidates is not usable.
	require.Empty(t, sanitizeETag(`"abc", "def"`))
	require.Empty(t, sanitizeETag(`W/"abc", W/"def"`))
}

func TestSanitizeETag_EmptyQuotedAccepted(t *testing.T) {
	t.Parallel()

	// *etagc permits zero characters, so `""` is a well-formed if unusual
	// validator and replays harmlessly.
	require.Equal(t, `""`, sanitizeETag(`""`))
}

func TestSanitizeETag_ControlAndNonASCIIDropped(t *testing.T) {
	t.Parallel()

	require.Empty(t, sanitizeETag("\"abc\r\nX-Injected: 1\""))
	require.Empty(t, sanitizeETag("\"abc\tdef\""))
	require.Empty(t, sanitizeETag(`"caf é"`))
}
