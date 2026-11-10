package platformmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func renderCatalogCarousel(t *testing.T, carousel catalogCarousel) string {
	t.Helper()

	result := catalogCarouselResult(carousel)
	require.NotNil(t, result)
	rendered, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	return rendered
}

func TestCatalogCarouselShowsOneCardPerServer(t *testing.T) {
	t.Parallel()

	rendered := renderCatalogCarousel(t, buildCatalogCarousel([]CatalogCandidate{
		{ProviderKey: "provider", CatalogRef: "linear/mcp", Name: "Linear", Description: "Issues and projects.", AuthSetup: "dcr"},
		{ProviderKey: "provider", CatalogRef: "docs/mcp", Name: "Docs Search", AuthSetup: "unknown"},
	}, &catalogCardProject{Slug: "support", Added: nil}, false))

	require.Equal(t, 2, strings.Count(rendered, `<li class="slide">`))
	require.Contains(t, rendered, `<ul class="track" tabindex="0" aria-label="MCP catalog servers for project support">`)
	require.Contains(t, rendered, "<h2>Linear</h2>")
	require.Contains(t, rendered, `<p class="line clamp">Issues and projects.</p>`)
	require.NotContains(t, rendered, `class="tags"`, "cards carry no sign-in or transport badges")
	require.Contains(t, rendered, `<span aria-hidden="true">DS</span>`, "a server without a stored logo shows its initials")
	require.Contains(t, rendered, `data-message="Add the Linear MCP server from the catalog to the support project (provider_key provider, catalog_ref linear/mcp)."`)
	require.NotContains(t, rendered, "More servers match")
	require.NotContains(t, rendered, "<img")
}

func TestCatalogCarouselMarksServersAlreadyInTheProject(t *testing.T) {
	t.Parallel()

	project := &catalogCardProject{Slug: "support", Added: map[catalogServerKey]catalogAddedServer{
		{providerKey: "provider", catalogRef: "linear/mcp"}: {Logo: "data:image/png;base64,AAAA", OpenURL: "https://dashboard.example.test/org/projects/support/mcp/linear"},
		{providerKey: "provider", catalogRef: "docs/mcp"}:   {Logo: "", OpenURL: ""},
	}}
	carousel := buildCatalogCarousel([]CatalogCandidate{
		{ProviderKey: "provider", CatalogRef: "linear/mcp", Name: "Linear"},
		{ProviderKey: "provider", CatalogRef: "docs/mcp", Name: "Docs"},
		{ProviderKey: "other", CatalogRef: "linear/mcp", Name: "Linear Mirror"},
	}, project, true)

	added := carousel.Slides[0]
	require.Equal(t, cardStatus{Label: "In project", Tone: cardToneDone}, added.Eyebrow)
	require.Equal(t, &cardAction{Label: "Open", URL: "https://dashboard.example.test/org/projects/support/mcp/linear"}, added.Open)
	require.Empty(t, added.AddMessage)

	withoutPage := carousel.Slides[1]
	require.Equal(t, "In project", withoutPage.Eyebrow.Label)
	require.Nil(t, withoutPage.Open)
	require.Empty(t, withoutPage.AddMessage, "an added server never offers Add again")

	other := carousel.Slides[2]
	require.Equal(t, "Catalog", other.Eyebrow.Label, "the same reference from another provider is a different server")
	require.NotEmpty(t, other.AddMessage)

	rendered := renderCatalogCarousel(t, carousel)
	require.Contains(t, rendered, `<img class="logo" src="data:image/png;base64,AAAA" alt="">`)
	require.Contains(t, rendered, `<a class="button" href="https://dashboard.example.test/org/projects/support/mcp/linear" target="_blank" rel="noopener noreferrer">Open`)
	require.Equal(t, 1, strings.Count(rendered, "data-message="))
	require.Contains(t, rendered, "More servers match. Ask for the next page to see them.")
}

func TestCatalogCarouselWithoutAProjectAsksForOne(t *testing.T) {
	t.Parallel()

	rendered := renderCatalogCarousel(t, buildCatalogCarousel([]CatalogCandidate{{ProviderKey: "provider", CatalogRef: "linear/mcp", Name: "Linear"}}, nil, false))
	require.Contains(t, rendered, `aria-label="MCP catalog servers">`)
	require.Contains(t, rendered, "from the catalog to a project (provider_key provider")
	require.NotContains(t, rendered, "In project")
}

func TestCatalogCarouselEmptyResult(t *testing.T) {
	t.Parallel()

	rendered := renderCatalogCarousel(t, buildCatalogCarousel(nil, nil, false))
	require.Contains(t, rendered, `<p class="empty">No catalog servers match this search.</p>`)
	require.NotContains(t, rendered, `class="track"`)
	require.NotContains(t, rendered, "data-message=")
}

func TestCatalogCarouselEscapesUntrustedText(t *testing.T) {
	t.Parallel()

	hostile := `<img src=x onerror="alert(1)">`
	rendered := renderCatalogCarousel(t, buildCatalogCarousel([]CatalogCandidate{
		{ProviderKey: `"><script>alert(2)</script>`, CatalogRef: hostile, Name: hostile, Description: `</p><script>alert(3)</script>`},
	}, &catalogCardProject{Slug: `"><b>x</b>`, Added: nil}, false))
	require.NotContains(t, rendered, "<img")
	require.NotContains(t, rendered, "<script")
	require.NotContains(t, rendered, "<b>")
	require.Contains(t, rendered, "&lt;img src=x onerror=&#34;alert(1)&#34;&gt;")

	unsafe := renderCatalogCarousel(t, buildCatalogCarousel([]CatalogCandidate{{ProviderKey: "provider", CatalogRef: "linear/mcp", Name: "Linear"}}, &catalogCardProject{Slug: "support", Added: map[catalogServerKey]catalogAddedServer{
		{providerKey: "provider", catalogRef: "linear/mcp"}: {Logo: "", OpenURL: "javascript:alert(4)"},
	}}, false))
	require.NotContains(t, unsafe, "javascript:")
	require.Contains(t, unsafe, `href="#ZgotmplZ"`)
}

// searchTestCatalog returns fixed search results.
type searchTestCatalog struct {
	testCatalog
	candidates []CatalogCandidate
}

func (c searchTestCatalog) Search(context.Context, string) ([]CatalogCandidate, error) {
	return c.candidates, nil
}

// The carousel rides in _meta only: the structured content and the text every
// other client reads stay exactly the search output.
func TestCatalogSearchCarriesTheCarouselOnlyInMeta(t *testing.T) {
	t.Parallel()

	candidates := []CatalogCandidate{{ProviderKey: "provider", CatalogRef: "linear/mcp", Name: "Linear", Description: "Issues.", AuthSetup: "dcr"}}
	codec, err := newCatalogCursorCodec("test-cursor-key")
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "catalog-carousel"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerCatalogTools(reg, searchTestCatalog{candidates: candidates}, allowBudget(), codec, nil, nil)
	session := connectTestClient(t, server)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_mcp_catalog", Arguments: map[string]any{"query": "linear"}})
	require.NoError(t, err)
	require.False(t, result.IsError)

	carousel, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, carousel, "<h2>Linear</h2>")

	want, err := json.Marshal(SearchCatalogOutput{Candidates: candidates})
	require.NoError(t, err)
	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(structured))
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, string(want), text.Text)
	require.NotContains(t, text.Text, "<")

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	require.Equal(t, cardResourceURI, tools.Tools[0].Meta["openai/outputTemplate"])
}

// A search for a project marks the servers that project already has, with
// their stored logo and a link to their dashboard page.
func TestCatalogSearchMarksServersTheProjectHas(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_carousel")
	require.NoError(t, err)

	providerKey := "browser-catalog-registry-" + uuid.NewString()
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	icons := &recordingServerIcons{}
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	store = store.WithServerIcons(icons)
	storage := assetstest.NewTestBlobStore(t)
	dashboardURL, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	added := CatalogCandidate{Name: "Reviewed MCP", ProviderKey: providerKey, CatalogRef: "app.example/mcp", SetupIntent: "dashboard_source_settings", AuthSetup: "dcr"}
	other := CatalogCandidate{Name: "Other MCP", ProviderKey: providerKey, CatalogRef: "other.example/mcp", SetupIntent: "dashboard_source_settings", AuthSetup: "none"}
	catalog := searchTestCatalog{
		testCatalog: testCatalog{details: CatalogDetails{CatalogCandidate: added, Transport: "streamable-http", remoteURL: "https://reviewed.example.test/mcp"}},
		candidates:  []CatalogCandidate{added, other},
	}
	registrations := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, store).
		WithDashboardURL(dashboardURL).
		WithCardLogos(testenv.NewLogger(t), conn, storage)
	codec, err := newCatalogCursorCodec("test-cursor-key")
	require.NoError(t, err)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerCatalogRegistrationTool(reg, registrations, NewOnboardingService(conn))
	registerCatalogTools(reg, catalog, allowBudget(), codec, nil, registrations)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	reg.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})
	session := connectTestClient(t, reg.server)

	search := func(projectSlug string) string {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_mcp_catalog", Arguments: map[string]any{"project_slug": projectSlug}})
		require.NoError(t, err)
		require.False(t, result.IsError)
		carousel, ok := result.Meta[cardMetaKey].(string)
		require.True(t, ok)
		return carousel
	}

	before := search(project.Slug)
	require.Equal(t, 2, strings.Count(before, "data-message="), "nothing is added yet")
	require.NotContains(t, before, "In project")

	logo := testPNG(t)
	icons.fetched = func(mcpServerID uuid.UUID) {
		storeLogo(t, conn, storage, project, principal.OrganizationID, mcpServerID, "image/png", logo)
	}
	callRegisterCatalogCard(t, ctx, session, project, providerKey)

	after := search(project.Slug)
	require.Contains(t, after, "In project")
	require.Contains(t, after, `<img class="logo" src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(logo)+`" alt="">`)
	require.Contains(t, after, `href="https://dashboard.example.test/`)
	require.Contains(t, after, "/projects/"+project.Slug+"/mcp/")
	require.Equal(t, 1, strings.Count(after, "data-message="), "only the server not yet added offers Add")
	require.Contains(t, after, "Add the Other MCP MCP server from the catalog to the "+project.Slug+" project")

	unknown := search("no-such-project")
	require.NotContains(t, unknown, "In project", "a project that does not resolve shows no added state")
}
