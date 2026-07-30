package externalmcp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// setupDocsFixture picks a published guide that exercises every lookup key
// unambiguously: its slug, one registry alias, and one endpoint URL all resolve
// to that guide alone. Deriving the fixture from the SDK rather than naming a
// vendor keeps these tests pinned to the endpoint's resolution and mapping
// behaviour, so re-publishing the guide catalog can't silently break them.
func setupDocsFixture(t *testing.T) (guides.Guide, string, guides.Remote) {
	t.Helper()

	for _, guide := range guides.Guides() {
		bySlug := guides.Resolve(string(guide.Slug))
		if len(bySlug) != 1 || bySlug[0].Kind != guides.MatchSlug {
			continue
		}

		for _, alias := range guide.Aliases {
			byAlias := guides.Resolve(alias)
			if len(byAlias) == 0 || byAlias[0].Kind != guides.MatchAlias {
				continue
			}
			if !allMatchGuide(byAlias, guide.Slug) {
				continue
			}

			for _, remote := range guide.Remotes {
				byURL := guides.ByURL(remote.URL)
				if len(byURL) != 1 || byURL[0].Ref != (guides.ServerRef{Guide: guide.Slug, Remote: remote.ID}) {
					continue
				}

				return guide, alias, remote
			}
		}
	}

	t.Fatal("no published setup guide resolves unambiguously by slug, alias, and endpoint URL")

	return guides.Guide{}, "", guides.Remote{}
}

func allMatchGuide(matches []guides.Match, slug guides.GuideSlug) bool {
	for _, match := range matches {
		if match.Ref.Guide != slug {
			return false
		}
	}

	return true
}

func TestExternalMCP_GetSetupDocs_ByRegistrySpecifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	guide, alias, _ := setupDocsFixture(t)

	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         nil,
		RegistrySpecifier: &alias,
	})
	require.NoError(t, err)
	require.Len(t, result.Guides, 1)

	got := result.Guides[0]
	require.Equal(t, string(guide.Slug), got.Slug)
	require.Equal(t, "alias", got.MatchKind)
	require.Equal(t, guide.Title, got.Title)
	require.Equal(t, guide.Summary, got.Summary)
	require.Equal(t, guide.Aliases, got.Aliases)
	require.Equal(t, string(guide.External), got.ExternalMarkdown)
	require.Equal(t, string(guide.Speakeasy), got.SpeakeasyMarkdown)
	require.NotEmpty(t, got.ExternalMarkdown)
	require.NotEmpty(t, got.SpeakeasyMarkdown)

	require.Len(t, got.Remotes, len(guide.Remotes))
	for i, remote := range guide.Remotes {
		require.Equal(t, string(remote.ID), got.Remotes[i].ID)
		require.Equal(t, remote.URL, got.Remotes[i].URL)
		require.Equal(t, remote.Transport, got.Remotes[i].TransportType)
		require.Equal(t, remote.Tenanted, got.Remotes[i].Tenanted)
	}
}

func TestExternalMCP_GetSetupDocs_ByGuideSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	guide, _, _ := setupDocsFixture(t)

	slug := string(guide.Slug)
	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         nil,
		RegistrySpecifier: &slug,
	})
	require.NoError(t, err)
	require.Len(t, result.Guides, 1)
	require.Equal(t, slug, result.Guides[0].Slug)
	require.Equal(t, "slug", result.Guides[0].MatchKind)
	// A slug names the guide, not one of its endpoints.
	require.Empty(t, result.Guides[0].MatchedRemoteIds)
}

func TestExternalMCP_GetSetupDocs_ByServerURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	guide, _, remote := setupDocsFixture(t)

	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         &remote.URL,
		RegistrySpecifier: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Guides, 1)
	require.Equal(t, string(guide.Slug), result.Guides[0].Slug)
	require.Equal(t, "endpoint", result.Guides[0].MatchKind)
	require.Equal(t, []string{string(remote.ID)}, result.Guides[0].MatchedRemoteIds)
}

// The catalog knows a server's endpoint URL and its registry identifier, so it
// can send both. The two must collapse to a single guide.
func TestExternalMCP_GetSetupDocs_BothKeysResolveToOneGuide(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	guide, alias, remote := setupDocsFixture(t)

	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         &remote.URL,
		RegistrySpecifier: &alias,
	})
	require.NoError(t, err)
	require.Len(t, result.Guides, 1)
	require.Equal(t, string(guide.Slug), result.Guides[0].Slug)
	// The identifier is checked ahead of the URL, so it wins the match kind.
	require.Equal(t, "alias", result.Guides[0].MatchKind)
	require.Contains(t, result.Guides[0].MatchedRemoteIds, string(remote.ID))
}

// A messy URL — surrounding whitespace, upper-case scheme, trailing slash —
// still matches.
func TestExternalMCP_GetSetupDocs_NormalizesServerURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	guide, _, remote := setupDocsFixture(t)

	messy := "  " + strings.Replace(remote.URL, "https://", "HTTPS://", 1) + "/  "
	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         &messy,
		RegistrySpecifier: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Guides, 1)
	require.Equal(t, string(guide.Slug), result.Guides[0].Slug)
}

func TestExternalMCP_GetSetupDocs_UnknownServerReturnsNoGuides(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)

	unknownURL := "https://mcp.example.invalid/mcp"
	unknownSpecifier := "com.example.invalid/nothing-here"
	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         &unknownURL,
		RegistrySpecifier: &unknownSpecifier,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Guides)
	require.Empty(t, result.Guides)
}

func TestExternalMCP_GetSetupDocs_MissingLookupKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)

	blank := "   "
	_, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         &blank,
		RegistrySpecifier: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}
