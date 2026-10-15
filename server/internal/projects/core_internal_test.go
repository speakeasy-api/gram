package projects

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A derived slug longer than the column allows is cut to fit rather than
// refused, and a hyphen left at the cut is trimmed. The name limit keeps this
// unreachable through ProjectSlug today, so it is pinned here directly: it is
// what keeps a raised name limit from becoming a slug constraint violation.
func TestDeriveProjectSlugCutsToTheSlugLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		slug string
	}{
		{name: "within limit", in: "Support Team", slug: "support-team"},
		{name: "exactly at limit", in: strings.Repeat("a", ProjectSlugMaxLength), slug: strings.Repeat("a", ProjectSlugMaxLength)},
		{name: "cut", in: strings.Repeat("a", ProjectSlugMaxLength+5), slug: strings.Repeat("a", ProjectSlugMaxLength)},
		// 39 letters then a space: the cut lands just after the hyphen the
		// space became, which must not survive as a trailing hyphen.
		{name: "trailing hyphen trimmed", in: strings.Repeat("b", ProjectSlugMaxLength-1) + " tail", slug: strings.Repeat("b", ProjectSlugMaxLength-1)},
		{name: "no letters or digits", in: strings.Repeat("!", ProjectSlugMaxLength+5), slug: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slug := deriveProjectSlug(tc.in)
			require.Equal(t, tc.slug, slug)
			require.LessOrEqual(t, len(slug), ProjectSlugMaxLength)
		})
	}
}
