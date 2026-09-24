package httpcache

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// testMaxETagLength mirrors the cap the cimd package persists ETags under.
const testMaxETagLength = 256

func TestSanitizeETag_StrongAndWeakPreservedVerbatim(t *testing.T) {
	t.Parallel()

	// The validator is replayed byte for byte: If-None-Match uses weak
	// comparison, so rewriting a W/ prefix would break revalidation against
	// hosts that emit one.
	require.Equal(t, `"abc123"`, SanitizeETag(`"abc123"`, testMaxETagLength))
	require.Equal(t, `W/"abc123"`, SanitizeETag(`W/"abc123"`, testMaxETagLength))
	require.Equal(t, `"abc123"`, SanitizeETag(`  "abc123"  `, testMaxETagLength))
}

func TestSanitizeETag_EmptyDropped(t *testing.T) {
	t.Parallel()

	require.Empty(t, SanitizeETag("", testMaxETagLength))
	require.Empty(t, SanitizeETag("   ", testMaxETagLength))
}

func TestSanitizeETag_OversizedDropped(t *testing.T) {
	t.Parallel()

	atLimit := `"` + strings.Repeat("a", testMaxETagLength-2) + `"`
	require.Len(t, atLimit, testMaxETagLength)
	require.Equal(t, atLimit, SanitizeETag(atLimit, testMaxETagLength))
	require.Empty(t, SanitizeETag(`"`+strings.Repeat("a", testMaxETagLength-1)+`"`, testMaxETagLength))
}

func TestSanitizeETag_WildcardDropped(t *testing.T) {
	t.Parallel()

	// "ETag: *" is not a valid response header, and replaying it as
	// "If-None-Match: *" would match whenever the host has any
	// representation at all, so every revalidation would answer 304 and the
	// cached document could never be superseded.
	require.Empty(t, SanitizeETag("*", testMaxETagLength))
	require.Empty(t, SanitizeETag("W/*", testMaxETagLength))
}

func TestSanitizeETag_UnquotedDropped(t *testing.T) {
	t.Parallel()

	require.Empty(t, SanitizeETag("abc123", testMaxETagLength))
	require.Empty(t, SanitizeETag(`"abc123`, testMaxETagLength))
	require.Empty(t, SanitizeETag(`abc123"`, testMaxETagLength))
	require.Empty(t, SanitizeETag(`"`, testMaxETagLength))
}

func TestSanitizeETag_InteriorSpaceDropped(t *testing.T) {
	t.Parallel()

	// etagc admits neither SP nor DQUOTE, so a spaced tag is malformed even
	// though a space is legal elsewhere in a header value.
	require.Empty(t, SanitizeETag(`"abc def"`, testMaxETagLength))
	require.Empty(t, SanitizeETag(`W/"abc def"`, testMaxETagLength))
}

func TestSanitizeETag_ListDropped(t *testing.T) {
	t.Parallel()

	// If-None-Match here asks about one specific stored document, so a list
	// of candidates is not usable.
	require.Empty(t, SanitizeETag(`"abc", "def"`, testMaxETagLength))
	require.Empty(t, SanitizeETag(`W/"abc", W/"def"`, testMaxETagLength))
}

func TestSanitizeETag_EmptyQuotedAccepted(t *testing.T) {
	t.Parallel()

	// *etagc permits zero characters, so `""` is a well-formed if unusual
	// validator and replays harmlessly.
	require.Equal(t, `""`, SanitizeETag(`""`, testMaxETagLength))
}

func TestSanitizeETag_ControlAndNonASCIIDropped(t *testing.T) {
	t.Parallel()

	require.Empty(t, SanitizeETag("\"abc\r\nX-Injected: 1\"", testMaxETagLength))
	require.Empty(t, SanitizeETag("\"abc\tdef\"", testMaxETagLength))
	require.Empty(t, SanitizeETag(`"caf é"`, testMaxETagLength))
}
