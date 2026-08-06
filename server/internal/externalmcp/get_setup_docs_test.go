package externalmcp_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const callbackTemplateKey = "{{ gram.oauth.callback_url }}"

// setupDocsFixture picks a published guide that resolves unambiguously by both
// lookup keys and carries the callback template key. Deriving it from the SDK
// rather than naming a vendor pins these tests to endpoint behaviour, not to the
// catalog.
func setupDocsFixture(t *testing.T) (guides.Guide, string, guides.Remote) {
	t.Helper()

	for _, guide := range guides.Guides() {
		alias, ok := unambiguousAlias(guide)
		if !ok || !strings.Contains(string(guide.External)+string(guide.Speakeasy), callbackTemplateKey) {
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

	t.Fatal("no published setup guide carries the callback template key and resolves unambiguously by both lookup keys")

	return guides.Guide{}, "", guides.Remote{}
}

// unambiguousAlias returns a registry alias that resolves to this guide and to no
// other.
func unambiguousAlias(guide guides.Guide) (string, bool) {
	for _, alias := range guide.Aliases {
		matches := guides.Resolve(alias)
		if len(matches) == 0 || matches[0].Kind != guides.MatchAlias {
			continue
		}
		if slices.ContainsFunc(matches, func(m guides.Match) bool { return m.Ref.Guide != guide.Slug }) {
			continue
		}

		return alias, true
	}

	return "", false
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

	// The callback URL belongs to the deployment, so the endpoint substitutes it.
	vars := guides.Vars{OAuthCallbackURL: testCallbackURL}
	require.Equal(t, string(guide.RenderExternal(vars)), got.ExternalMarkdown)
	require.Equal(t, string(guide.RenderSpeakeasy(vars)), got.SpeakeasyMarkdown)
	require.NotEmpty(t, got.ExternalMarkdown)
	require.NotEmpty(t, got.SpeakeasyMarkdown)
	require.NotContains(t, got.ExternalMarkdown, callbackTemplateKey)
	require.NotContains(t, got.SpeakeasyMarkdown, callbackTemplateKey)
	require.Contains(t, got.ExternalMarkdown+got.SpeakeasyMarkdown, testCallbackURL)

	require.Len(t, got.Remotes, len(guide.Remotes))
	for i, remote := range guide.Remotes {
		require.Equal(t, string(remote.ID), got.Remotes[i].ID)
		require.Equal(t, remote.URL, got.Remotes[i].URL)
		require.Equal(t, remote.Transport, got.Remotes[i].TransportType)
		require.Equal(t, remote.Tenanted, got.Remotes[i].Tenanted)
	}
}

// registry_specifier means a registry specifier, the identifier listCatalog
// returns per entry. The SDK also indexes guides by docs-catalog identity (a
// guide slug, a canonical "slug/remote-id" ref), and neither is accepted here:
// they name a guide rather than a server in a registry, and no caller of this
// endpoint holds one.
func TestExternalMCP_GetSetupDocs_DocsCatalogIdentifiersAreNotAccepted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	guide, _, remote := setupDocsFixture(t)

	for _, specifier := range []string{
		string(guide.Slug),
		guides.ServerRef{Guide: guide.Slug, Remote: remote.ID}.String(),
	} {
		// The SDK does resolve these; the endpoint is what declines them.
		require.NotEmpty(t, guides.Resolve(specifier), "fixture no longer resolves %q", specifier)

		result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
			SessionToken:      nil,
			ApikeyToken:       nil,
			ProjectSlugInput:  nil,
			ServerURL:         nil,
			RegistrySpecifier: &specifier,
		})
		require.NoError(t, err, "specifier %q", specifier)
		require.Empty(t, result.Guides, "specifier %q", specifier)
	}
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
	require.Equal(t, string(remote.ID), conv.PtrValOr(result.Guides[0].MatchedRemoteID, ""))
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
	// The identifier is resolved first, but the URL pinned an exact endpoint, so
	// the more specific of the two matches wins the match kind.
	require.Equal(t, "endpoint", result.Guides[0].MatchKind)
	require.Equal(t, string(remote.ID), conv.PtrValOr(result.Guides[0].MatchedRemoteID, ""))
}

// A messy URL still matches: surrounding whitespace, upper-case scheme, trailing
// slash.
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

// The identifier is resolved before the URL, so ordering has to be sorted for
// rather than inherited from resolution order: a guide the caller only named
// must not outrank one whose exact endpoint the caller supplied.
func TestExternalMCP_GetSetupDocs_EndpointMatchOutranksGuideLevelMatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	urlGuide, _, remote := setupDocsFixture(t)

	var otherAlias string
	var otherSlug guides.GuideSlug
	for _, guide := range guides.Guides() {
		if guide.Slug == urlGuide.Slug {
			continue
		}
		if alias, ok := unambiguousAlias(guide); ok {
			otherAlias, otherSlug = alias, guide.Slug
			break
		}
	}
	require.NotEmpty(t, otherAlias, "no second published guide resolves unambiguously by registry alias")

	result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ServerURL:         &remote.URL,
		RegistrySpecifier: &otherAlias,
	})
	require.NoError(t, err)
	require.Len(t, result.Guides, 2)

	require.Equal(t, string(urlGuide.Slug), result.Guides[0].Slug)
	require.Equal(t, "endpoint", result.Guides[0].MatchKind)
	require.Equal(t, string(otherSlug), result.Guides[1].Slug)
	require.Equal(t, "alias", result.Guides[1].MatchKind)
}

// Sweeping every published registry alias pins the two invariants that the
// unexposed SDK indexes used to break. Provenance is keyed by the section titles
// of the upstream docs a guide was derived from, so it answered a lookup for one
// vendor's server with another vendor's guide, and it also leaked an endpoint
// into matched_remote_id whenever a guide's docs happened to carry a section
// titled with the alias. So: no alias reports a kind other than "alias", and no
// alias claims to have selected one of the guide's endpoints.
func TestExternalMCP_GetSetupDocs_AliasLookupsSelectNoEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)

	var swept int
	for _, guide := range guides.Guides() {
		for _, alias := range guide.Aliases {
			result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{
				SessionToken:      nil,
				ApikeyToken:       nil,
				ProjectSlugInput:  nil,
				ServerURL:         nil,
				RegistrySpecifier: &alias,
			})
			require.NoError(t, err, "alias %q", alias)
			require.NotEmpty(t, result.Guides, "alias %q", alias)
			swept++

			for _, got := range result.Guides {
				require.Equal(t, "alias", got.MatchKind, "alias %q matched guide %q", alias, got.Slug)
				require.Nil(t, got.MatchedRemoteID, "alias %q matched guide %q", alias, got.Slug)
			}
		}
	}

	require.NotZero(t, swept, "no published guide declares a registry alias")
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
