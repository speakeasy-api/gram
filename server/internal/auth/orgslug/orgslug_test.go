package orgslug_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
)

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercases and joins words", input: "Acme Inc", want: "acme-inc"},
		{name: "trims surrounding space", input: "  Acme Inc  ", want: "acme-inc"},
		{name: "collapses runs of separators", input: "Acme   ---   Inc", want: "acme-inc"},
		{name: "drops trailing punctuation", input: "Acme, Inc.", want: "acme-inc"},
		{name: "apostrophes become separators", input: "Bob's Bakery", want: "bob-s-bakery"},
		{name: "non-ASCII characters become separators", input: "Café Zoë", want: "caf-zo"},
		{name: "keeps digits", input: "Acme 2 Go", want: "acme-2-go"},
		{name: "ampersand becomes a separator", input: "Acme & Sons", want: "acme-sons"},
		{name: "punctuation only yields nothing", input: "- _ -", want: ""},
		{name: "han yields nothing", input: "顶尖科技", want: ""},
		{name: "katakana yields nothing", input: "アクメ株式会社", want: ""},
		{name: "cyrillic yields nothing", input: "Акме", want: ""},
		{name: "keeps the latin part of a mixed name", input: "Acme 株式会社", want: "acme"},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, orgslug.Slugify(tt.input), tt.name)
	}
}

func TestBaseUsesTheSlugWhenThereIsOne(t *testing.T) {
	t.Parallel()

	base, err := orgslug.Base("Acme Inc")
	require.NoError(t, err)
	require.Equal(t, "acme-inc", base)
}

func TestBaseGeneratesWhenTheNameYieldsNoUsableSlug(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"顶尖科技", "アクメ株式会社", "Акме", "X 株式会社"} {
		base, err := orgslug.Base(name)
		require.NoError(t, err)
		require.Regexp(t, `^org-[a-z1-9]{8}$`, base, "name %q must get a generated base", name)
	}
}
