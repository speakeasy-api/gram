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
		{name: "drops apostrophes rather than splitting on them", input: "Bob's Bakery", want: "bobs-bakery"},
		{name: "drops curly apostrophes", input: "Bob\u2019s Bakery", want: "bobs-bakery"},
		{name: "folds accented latin", input: "Café Zoë", want: "cafe-zoe"},
		{name: "folds umlauts", input: "Grünwald", want: "grunwald"},
		{name: "spells out eszett", input: "Grünwald Straße", want: "grunwald-strasse"},
		{name: "spells out nordic letters", input: "Ørsted Æther", want: "orsted-aether"},
		{name: "keeps digits", input: "Acme 2 Go", want: "acme-2-go"},
		{name: "ampersand becomes a separator", input: "Acme & Sons", want: "acme-sons"},
		{name: "punctuation only yields nothing", input: "- _ -", want: ""},
		{name: "han yields nothing", input: "顶尖科技", want: ""},
		{name: "katakana yields nothing", input: "アクメ株式会社", want: ""},
		{name: "cyrillic yields nothing", input: "Акме", want: ""},
		{name: "keeps the latin part of a mixed name", input: "Acme 株式会社", want: "acme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, orgslug.Slugify(tt.input))
		})
	}
}

func TestBaseUsesTheSlugWhenThereIsOne(t *testing.T) {
	t.Parallel()

	base, err := orgslug.Base("Café Zoë's Grünwald")
	require.NoError(t, err)
	require.Equal(t, "cafe-zoes-grunwald", base)
}

// Names in a non-Latin script slugify to nothing, and a one-character slug is
// too little to identify an organization by. Both get a generated base rather
// than an unusable one.
func TestBaseGeneratesWhenTheNameYieldsNoUsableSlug(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"顶尖科技", "アクメ株式会社", "Акме", "X 株式会社"} {
		base, err := orgslug.Base(name)
		require.NoError(t, err)
		require.Regexp(t, `^org-[a-z1-9]{8}$`, base, "name %q must get a generated base", name)
	}
}

func TestBaseGeneratesADistinctBaseEachTime(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 50)
	for range 50 {
		base, err := orgslug.Base("顶尖科技")
		require.NoError(t, err)
		seen[base] = struct{}{}
	}
	require.Len(t, seen, 50, "generated bases must not collide with each other")
}
