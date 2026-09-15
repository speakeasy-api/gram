package setupcorpus_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/setupcorpus"
)

const callbackURL = "https://app.example.test/oauth/callback"

func build(t *testing.T) []platformmcp.SetupResource {
	t.Helper()
	resources, err := setupcorpus.Build(setupcorpus.Options{OAuthCallbackURL: callbackURL})
	require.NoError(t, err)
	require.NotEmpty(t, resources)
	return resources
}

// Every published guide must reach the corpus under a valid URI. A guide that
// silently failed to build would look identical to a provider we never covered.
func TestBuildCoversEveryPublishedGuide(t *testing.T) {
	t.Parallel()

	resources := build(t)

	providers := make(map[string]bool, len(resources))
	uris := make(map[string]bool, len(resources))
	for _, resource := range resources {
		require.NoError(t, platformmcp.ValidateSetupResource(resource))
		require.False(t, uris[resource.URI], "duplicate resource URI %s", resource.URI)
		uris[resource.URI] = true
		providers[resource.Provider] = true
	}

	for _, slug := range guides.Slugs() {
		require.True(t, providers[string(slug)], "guide %q produced no setup resource", slug)
		require.True(t, uris[platformmcp.SetupResourceURI(string(slug), setupcorpus.ProviderSetupIntent)], "guide %q has no provider setup resource", slug)
	}
}

// The pinned export is the only source of setup content, so its citation must
// name a real version and its links must be followable.
func TestBuildCitesThePinnedExport(t *testing.T) {
	t.Parallel()

	for _, resource := range build(t) {
		require.Equal(t, setupcorpus.Owner, resource.Owner)
		require.Equal(t, "mcp-setup-docs/go@"+setupcorpus.PinnedVersion, resource.Source)
		require.Contains(t, resource.Text, resource.Source, "the guide text carries its own citation")
		for _, link := range resource.Links {
			require.True(t, strings.HasPrefix(link, "https://"), "resource %s offers a non-https link %q", resource.URI, link)
		}
	}
}

// The deployment's callback URL is substituted, not left as a template key: a
// reader who is shown "{{ gram.oauth.callback_url }}" cannot complete setup.
func TestBuildRendersTheCallbackURL(t *testing.T) {
	t.Parallel()

	rendered := false
	for _, resource := range build(t) {
		require.NotContains(t, resource.Text, "{{ gram.oauth.callback_url }}")
		if strings.Contains(resource.Text, callbackURL) {
			rendered = true
		}
	}
	require.True(t, rendered, "at least one guide references the callback URL")
}

// Every guide is published on the documentation site under its slug, so a
// reader handed a citation can open the page rather than only be quoted at.
func TestBuildLinksEachGuideToItsPublishedPage(t *testing.T) {
	t.Parallel()

	for _, resource := range build(t) {
		require.Equal(t,
			"https://www.speakeasy.com/docs/ai-control-plane/guides/"+resource.Provider,
			resource.DocsURL,
			"resource %s", resource.URI)
		require.Contains(t, resource.Text, "- Speakeasy docs: "+resource.DocsURL,
			"the guide carries its own page link, so a model handed only the text still has it")
	}
}

// The citation header is prose about the guide, not guide content. Indexing it
// would answer a setup question with an owner-and-dates block and would let
// every guide match on "source" or "observed".
func TestBuildSeparatesCitationHeaderFromIndexedBody(t *testing.T) {
	t.Parallel()

	for _, resource := range build(t) {
		require.NotEmpty(t, resource.Body)
		require.True(t, strings.HasSuffix(resource.Text, resource.Body), "resource %s body is the tail of its text", resource.URI)
		require.Contains(t, resource.Text, "- Owner:")
		require.NotContains(t, resource.Body, "- Owner:")
		require.NotContains(t, resource.Body, "pinned reviewed export")
	}
}

// An alias is how a caller names a provider when they are holding a registry
// entry rather than a guide. One alias naming two providers would make that
// lookup ambiguous, and the reader would be cited whichever guide sorted first.
func TestAliasesNameOneProviderEach(t *testing.T) {
	t.Parallel()

	owner := map[string]string{}
	for _, resource := range build(t) {
		for _, alias := range resource.Aliases {
			if existing, seen := owner[alias]; seen {
				require.Equalf(t, existing, resource.Provider, "alias %q names both %q and %q", alias, existing, resource.Provider)
				continue
			}
			owner[alias] = resource.Provider
		}
	}
}

// The cited version and the version the binary actually embeds are two
// different facts, and only this test keeps them the same one.
func TestPinnedVersionMatchesGoMod(t *testing.T) {
	t.Parallel()

	// setupcorpus → platformmcp → internal → server → repository root.
	gomod, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(gomod), setupcorpus.ModulePath+" "+setupcorpus.PinnedVersion,
		"setupcorpus.PinnedVersion must name the version go.mod resolves; bump both together")
}

// Freshness CI: the pinned export must be recent enough that its guides are
// still inside their revalidation window. This failing is the signal to review
// upstream and bump the pinned version — it is what makes the 90-day review
// commitment enforceable rather than aspirational.
func TestPinnedCorpusIsWithinItsRevalidationWindow(t *testing.T) {
	t.Parallel()

	now := time.Now()
	for _, resource := range build(t) {
		require.Falsef(t, now.After(resource.RevalidateBy),
			"setup guide %s was last observed %s and was due revalidation %s: review the guide upstream in mcp-setup-docs and bump the pinned go/vX.Y.Z module version",
			resource.URI, resource.ObservedAt.Format(time.DateOnly), resource.RevalidateBy.Format(time.DateOnly))
	}
}
