package setupguides

import (
	"os"
	"path/filepath"
	"testing"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"
	"github.com/stretchr/testify/require"
)

// The reported version and the version the binary actually embeds are two
// different facts, and only this test keeps them the same one.
func TestPinnedVersionMatchesGoMod(t *testing.T) {
	t.Parallel()

	// setupguides → internal → server → repository root.
	gomod, err := os.ReadFile(filepath.Join("..", "..", "..", "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(gomod), ModulePath+" "+PinnedVersion,
		"setupguides.PinnedVersion must name the version go.mod resolves; bump both together")
}

func TestListCoversEveryPinnedGuide(t *testing.T) {
	t.Parallel()

	pinned := guides.Guides()
	list := List()
	require.Len(t, list, len(pinned))
	seen := make(map[string]struct{}, len(list))
	for i, guide := range list {
		require.NotEmpty(t, guide.Slug)
		require.NotEmpty(t, guide.Title)
		require.Equal(t, string(pinned[i].Slug), guide.Slug, "List keeps the export's order")
		require.Equal(t, pinned[i].Title, guide.Title)
		_, duplicate := seen[guide.Slug]
		require.False(t, duplicate, "slug %q is listed twice", guide.Slug)
		seen[guide.Slug] = struct{}{}
	}
	for _, slug := range guides.Slugs() {
		_, ok := seen[string(slug)]
		require.True(t, ok, "pinned guide %q is missing from List", slug)
	}
}

func TestLookupMatchesExactSlug(t *testing.T) {
	t.Parallel()

	guide, ok := Lookup("slack")
	require.True(t, ok)
	require.Equal(t, "slack", guide.Slug)
	require.NotEmpty(t, guide.Title)
}

func TestLookupIgnoresUnknownSlugs(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{"", "example-mcp", "slack-enterprise", "Slack"} {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()
			_, ok := Lookup(slug)
			require.False(t, ok)
		})
	}
}

func TestForEndpointFindsTheDocumentedGuide(t *testing.T) {
	t.Parallel()

	guide, ok := ForEndpoint("https://mcp.slack.com/mcp")
	require.True(t, ok)
	require.Equal(t, "slack", guide.Slug)
}

func TestForEndpointIgnoresUnknownEndpoints(t *testing.T) {
	t.Parallel()

	_, ok := ForEndpoint("https://mcp.example.com/mcp")
	require.False(t, ok)
}

func TestUniqueGuideRejectsAmbiguousMatches(t *testing.T) {
	t.Parallel()

	slack := guides.Match{Ref: guides.ServerRef{Guide: "slack", Remote: "hosted"}, Kind: guides.MatchEndpoint}
	slackOther := guides.Match{Ref: guides.ServerRef{Guide: "slack", Remote: "other"}, Kind: guides.MatchEndpoint}
	box := guides.Match{Ref: guides.ServerRef{Guide: "box", Remote: "hosted"}, Kind: guides.MatchEndpoint}

	slug, ok := uniqueGuide([]guides.Match{slack, slackOther})
	require.True(t, ok, "two remotes of one guide name one guide")
	require.Equal(t, guides.GuideSlug("slack"), slug)

	_, ok = uniqueGuide([]guides.Match{slack, box})
	require.False(t, ok)

	_, ok = uniqueGuide(nil)
	require.False(t, ok)
}
