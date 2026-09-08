package httpcache

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// freshnessTestPolicy uses the bounds the cimd package shipped with, so the
// behavior cases ported from there stay byte-identical.
var freshnessTestPolicy = FreshnessPolicy{
	Default: time.Hour,
	Min:     5 * time.Minute,
	Max:     24 * time.Hour,
}

// ttlReference is the fixed instant Expires-based cases are measured against,
// so the arithmetic in the test is exact rather than racing the clock.
var ttlReference = time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)

func headerWith(t *testing.T, pairs ...string) http.Header {
	t.Helper()

	require.Zero(t, len(pairs)%2, "pairs must be key/value")
	header := http.Header{}
	for i := 0; i < len(pairs); i += 2 {
		header.Add(pairs[i], pairs[i+1])
	}
	return header
}

func TestFreshnessPolicyTTL_MaxAgeHonoured(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "public, max-age=7200"), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeBelowFloorClamped(t *testing.T) {
	t.Parallel()

	// An upstream asking for 2 minutes reads as the floor, so an
	// attacker-triggerable endpoint can never be pushed into fetching on
	// every request.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=120"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeAboveCeilingClamped(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=31536000"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Max, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeZeroClampedToFloor(t *testing.T) {
	t.Parallel()

	// max-age=0 is a well-formed request to never cache. The floor applies
	// anyway: caching a public document is the point of this policy.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=0"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeQuoted(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `max-age="7200"`), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeAcrossRepeatedHeaders(t *testing.T) {
	t.Parallel()

	// RFC 9110 §5.3 lets a field repeat across lines; reading only the first
	// would miss this directive entirely.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "public",
		"Cache-Control", "max-age=7200",
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeCaseInsensitive(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "Max-Age=7200"), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_CommaInsideQuotedDirectiveIsNotADirective(t *testing.T) {
	t.Parallel()

	// The quoted argument of no-cache must not be split into a fragment that
	// reads as a max-age the host never granted.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `no-cache="Set-Cookie, max-age=99999"`), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_QuotedDirectiveDoesNotHideRealMaxAge(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `private="Set-Cookie, Authorization", max-age=7200`), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_EscapedQuoteInsideDirectiveHandled(t *testing.T) {
	t.Parallel()

	// A quoted-pair escaping a quote must not be read as closing the string,
	// which would put the rest of the value back in directive position.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `no-cache="a\", max-age=99999", max-age=7200`), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_UnmatchedQuoteFallsBackToDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, freshnessTestPolicy.Default, freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `max-age="7200`), ttlReference))
	require.Equal(t, freshnessTestPolicy.Default, freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `max-age=7200"`), ttlReference))
	require.Equal(t, freshnessTestPolicy.Default, freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `max-age=""7200""`), ttlReference))
}

func TestFreshnessPolicyTTL_MaxAgeOverflowFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// A value past the int64 nanosecond range must not wrap into a negative
	// duration, which the clamp would silently read as "expire immediately".
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=99999999999999999999"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeNegativeFallsBackToDefault(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=-30"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeSignedFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// delta-seconds is 1*DIGIT, so a sign is not part of the grammar even
	// where strconv would accept it.
	require.Equal(t, freshnessTestPolicy.Default, freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=+7200"), ttlReference))
	require.Equal(t, freshnessTestPolicy.Default, freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=-0"), ttlReference))
}

func TestFreshnessPolicyTTL_UnterminatedQuoteDiscardsWholeField(t *testing.T) {
	t.Parallel()

	// The directive boundaries after an unterminated quote are guesswork, so
	// the field is treated as no opinion rather than as a partial one.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", `max-age=7200, no-cache="unterminated`), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_OverflowingAgeReadsAsAncient(t *testing.T) {
	t.Parallel()

	// An Age too large to represent means the host is calling the response
	// ancient. Reading it as zero would hand a stale document a full day.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "99999999999999999999",
	), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)
}

func TestFreshnessPolicyTTL_AncientExpiresWithOverflowingAgeStaysAtFloor(t *testing.T) {
	t.Parallel()

	// Two "this is ancient" signals at once: an Expires decades before Date
	// makes the lifetime hugely negative, and the Age saturates. A plain
	// subtraction would wrap past MinInt64 into a large positive value the
	// clamp reads as maximum freshness — the floor is the only correct
	// answer.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Date", ttlReference.Format(http.TimeFormat),
		"Expires", ttlReference.AddDate(-32, 0, 0).Format(http.TimeFormat),
		"Age", "99999999999999999999",
	), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)

	// The merely-huge spelling wraps the same subtraction without saturating
	// anything first.
	ttl = freshnessTestPolicy.TTL(headerWith(t,
		"Date", ttlReference.Format(http.TimeFormat),
		"Expires", ttlReference.AddDate(-32, 0, 0).Format(http.TimeFormat),
		"Age", "8300000000",
	), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)
}

func TestFreshnessPolicyTTL_MalformedSharedMaxAgeFallsThroughToMaxAge(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "s-maxage=abc, max-age=7200"), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_MaxAgeMalformedFallsBackToDefault(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "max-age=soon"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_NoStoreIgnored(t *testing.T) {
	t.Parallel()

	// A deliberate RFC 9111 deviation: the document is public by definition,
	// so no-store carries no confidentiality meaning here and honouring it
	// would only defeat the floor.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "no-store, no-cache, must-revalidate"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_NoStoreDoesNotOverrideMaxAge(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "no-cache, max-age=7200"), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_SharedMaxAgePreferredOverMaxAge(t *testing.T) {
	t.Parallel()

	// RFC 9111 §5.2.2.10: consumers of this policy are shared caches, so
	// s-maxage wins.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Cache-Control", "s-maxage=7200, max-age=86400"), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_AgeDeductedFromMaxAge(t *testing.T) {
	t.Parallel()

	// The CDN case: a day of granted freshness that has already been sitting
	// in an intermediary for 22 hours leaves 2 hours, not 24.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "79200",
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_AgeExceedingLifetimeClampedToFloor(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "90000",
	), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)
}

func TestFreshnessPolicyTTL_ApparentAgeFromDateDeducted(t *testing.T) {
	t.Parallel()

	// An intermediary replaying a stale body without adding an Age header:
	// the Date it kept is what reveals how much freshness is really left.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Date", ttlReference.Add(-22*time.Hour).Format(http.TimeFormat),
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_LargerOfAgeAndApparentAgeWins(t *testing.T) {
	t.Parallel()

	// Age says 22 hours while Date implies only one, so Age wins.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "79200",
		"Date", ttlReference.Add(-time.Hour).Format(http.TimeFormat),
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)

	// Reversed: Date implies 22 hours while Age claims one.
	ttl = freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=86400",
		"Age", "3600",
		"Date", ttlReference.Add(-22*time.Hour).Format(http.TimeFormat),
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_FutureDateDoesNotExtendLifetime(t *testing.T) {
	t.Parallel()

	// A skewed origin clock implies a negative apparent age, which must not
	// be added back onto the lifetime.
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=7200",
		"Date", ttlReference.Add(time.Hour).Format(http.TimeFormat),
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_MalformedAgeIgnored(t *testing.T) {
	t.Parallel()

	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=7200",
		"Age", "ancient",
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_ExpiresMeasuredAgainstDate(t *testing.T) {
	t.Parallel()

	// An origin whose clock runs two hours fast still grants exactly the two
	// hours it meant to, because Expires is read relative to its own Date.
	originNow := ttlReference.Add(2 * time.Hour)
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Date", originNow.Format(http.TimeFormat),
		"Expires", originNow.Add(2*time.Hour).Format(http.TimeFormat),
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_ExpiresHonouredWhenNoMaxAge(t *testing.T) {
	t.Parallel()

	expires := ttlReference.Add(2 * time.Hour).Format(http.TimeFormat)
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Expires", expires), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_MaxAgePreferredOverExpires(t *testing.T) {
	t.Parallel()

	expires := ttlReference.Add(20 * time.Hour).Format(http.TimeFormat)
	ttl := freshnessTestPolicy.TTL(headerWith(t,
		"Cache-Control", "max-age=7200",
		"Expires", expires,
	), ttlReference)
	require.Equal(t, 2*time.Hour, ttl)
}

func TestFreshnessPolicyTTL_ExpiresInPastClampedToFloor(t *testing.T) {
	t.Parallel()

	expires := ttlReference.Add(-time.Hour).Format(http.TimeFormat)
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Expires", expires), ttlReference)
	require.Equal(t, freshnessTestPolicy.Min, ttl)
}

func TestFreshnessPolicyTTL_ExpiresZeroFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// "Expires: 0" is the common spelling of "already stale" and is not a
	// valid HTTP-date, so it reads as no opinion rather than as the floor.
	ttl := freshnessTestPolicy.TTL(headerWith(t, "Expires", "0"), ttlReference)
	require.Equal(t, freshnessTestPolicy.Default, ttl)
}

func TestFreshnessPolicyTTL_NoHeadersUsesDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, freshnessTestPolicy.Default, freshnessTestPolicy.TTL(http.Header{}, ttlReference))
}
