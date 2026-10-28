package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func renderCard(t *testing.T, card resultCard) string {
	t.Helper()

	result := cardResult(card)
	require.NotNil(t, result)
	rendered, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	return rendered
}

func TestCardResourceIsListedAndReadable(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "card-resource"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerCardResource(reg)
	session := connectTestClient(t, server)

	listed, err := session.ListResources(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, listed.Resources, 1)
	require.Equal(t, cardResourceURI, listed.Resources[0].URI)
	require.Equal(t, cardMIMEType, listed.Resources[0].MIMEType)

	read, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: cardResourceURI})
	require.NoError(t, err)
	require.Len(t, read.Contents, 1)
	require.Equal(t, cardMIMEType, read.Contents[0].MIMEType)
	require.Equal(t, cardShellHTML, read.Contents[0].Text)
	require.Equal(t, map[string]any{"prefersBorder": true}, read.Contents[0].Meta["ui"])

	_, assistant := reg.ResourceFor(AudienceAssistant, cardResourceURI)
	require.False(t, assistant, "the managed assistant renders no MCP Apps")
}

// The card view is served only where the tools that link to it are live.
func TestCardResourceFollowsTheLiveRegistrationTools(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	_, served := runtime.registrar.ResourceFor(AudienceExternal, cardResourceURI)
	require.False(t, served)
}

// The shell must render without any network load: the host's default
// content-security policy blocks one, and a card has nothing to fetch.
func TestCardShellLoadsNothingFromTheNetwork(t *testing.T) {
	t.Parallel()

	for _, load := range []string{"src=", "<link", "@import", "url(", "fetch(", "XMLHttpRequest"} {
		require.NotContains(t, cardShellHTML, load)
	}
}

// The live composition serves the card view next to the tools that link to it,
// so a missing resource registration cannot ship with the links intact.
func TestCardToolsLinkTheServedCardView(t *testing.T) {
	t.Parallel()

	registrations := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, &recordingRegistrationStore{}).
		WithDirectRemoteInspector(&testDirectRemoteInspector{})
	registrations.budgets.Registration = OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}
	server, _ := newServer(nil, nil, registrations, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	var served []string
	for resource, err := range connectTestClient(t, server).Resources(t.Context(), nil) {
		require.NoError(t, err)
		served = append(served, resource.URI)
	}
	require.Contains(t, served, cardResourceURI)

	linked := map[string]bool{}
	for _, tool := range listAdvertisedTools(t, server) {
		if tool.Meta == nil {
			continue
		}
		require.Equal(t, map[string]any{"resourceUri": cardResourceURI}, tool.Meta["ui"], tool.Name)
		require.Equal(t, cardResourceURI, tool.Meta["openai/outputTemplate"], tool.Name)
		encoded, err := json.Marshal(tool.Meta)
		require.NoError(t, err)
		require.Less(t, len(encoded), 128, "keep tool _meta small for the host tool budget")
		linked[tool.Name] = true
	}
	require.Equal(t, map[string]bool{"register_catalog_mcp": true, "register_remote_mcp": true, "attach_platform_mcp_identity_provider": true}, linked)
}

func TestCardEscapesUntrustedText(t *testing.T) {
	t.Parallel()

	card := serverCard(`<img src=x onerror="alert(1)">`, ResolvedProject{Name: `"><script>alert(2)</script>`, Slug: "project"}, nextActionSecureDashboardSetup, "https://dashboard.example.test/setup?a=1&b=2")
	rendered := renderCard(t, card)
	require.NotContains(t, rendered, "<img")
	require.NotContains(t, rendered, "<script")
	require.Contains(t, rendered, "&lt;img src=x onerror=&#34;alert(1)&#34;&gt;")
	require.Contains(t, rendered, "&lt;script&gt;")
	require.Contains(t, rendered, `href="https://dashboard.example.test/setup?a=1&amp;b=2"`)

	unsafe := renderCard(t, signInCard("Example", "javascript:alert(3)"))
	require.NotContains(t, unsafe, "javascript:")
	require.Contains(t, unsafe, `href="#ZgotmplZ"`)
}

func TestServerCardNextStep(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{Name: "Support", Slug: "support"}
	setupURL := "https://dashboard.example.test/setup"
	for _, tc := range []struct {
		nextAction string
		setupURL   string
		nextStep   string
		action     *cardAction
	}{
		{nextActionStartSetup, "", "Next: check that it is ready to use.", nil},
		{"ready", "", "Next: check that it is ready to use.", nil},
		{nextActionContinueDashboardSetup, setupURL, "Next: finish sign-in in the dashboard.", &cardAction{Label: "Finish sign-in", URL: setupURL}},
		{nextActionSecureDashboardSetup, setupURL, "Next: enter its credentials in the dashboard.", &cardAction{Label: "Open dashboard setup", URL: setupURL}},
		{nextActionContinueDashboardSetup, "", "Next: finish setup from the MCP server's page in the dashboard.", nil},
	} {
		card := serverCard("Example", project, tc.nextAction, tc.setupURL)
		require.Equal(t, "Example", card.Title)
		require.Equal(t, "Added to project Support", card.Subtitle)
		require.Equal(t, "Private until added to a plugin", card.Note)
		require.Equal(t, tc.nextStep, card.NextStep, tc.nextAction)
		require.Equal(t, tc.action, card.Action, tc.nextAction)
	}

	unnamed := serverCard("", ResolvedProject{Name: "", Slug: "support"}, "ready", "")
	require.Equal(t, "MCP server", unnamed.Title)
	require.Equal(t, "Added to project support", unnamed.Subtitle)
}

func TestSignInCardOpensTheAuthorizationURL(t *testing.T) {
	t.Parallel()

	authorizationURL := "https://dashboard.example.test/org/projects/p/mcp/example/inspect"
	rendered := renderCard(t, signInCard("Example", authorizationURL))
	require.Contains(t, rendered, `href="`+authorizationURL+`"`)
	require.Contains(t, rendered, ">Sign in to Example</a>")
	require.Equal(t, 1, strings.Count(rendered, "<a "), "the card offers one link")

	require.Equal(t, &cardAction{Label: "Sign in", URL: authorizationURL}, signInCard("", authorizationURL).Action)
}
