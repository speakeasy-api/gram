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
	require.Equal(t, map[string]any{"prefersBorder": false}, read.Contents[0].Meta["ui"])

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

	card := serverCard(`<img src=x onerror="alert(1)">`, ResolvedProject{Name: `"><script>alert(2)</script>`, Slug: "project"}, nextActionSecureDashboardSetup, "https://dashboard.example.test/setup?a=1&b=2", false, `https://example.test/mcp?x="><b>`)
	rendered := renderCard(t, card)
	require.NotContains(t, rendered, "<img")
	require.NotContains(t, rendered, "<script")
	require.Contains(t, rendered, "&lt;img src=x onerror=&#34;alert(1)&#34;&gt;")
	require.Contains(t, rendered, "&lt;script&gt;")
	require.Contains(t, rendered, `href="https://dashboard.example.test/setup?a=1&amp;b=2"`)
	require.NotContains(t, rendered, "<b>")
	require.Contains(t, rendered, `data-copy="https://example.test/mcp?x=&#34;&gt;&lt;b&gt;"`)

	unsafe := renderCard(t, signInCard("Example", "javascript:alert(3)"))
	require.NotContains(t, unsafe, "javascript:")
	require.Contains(t, unsafe, `href="#ZgotmplZ"`)
}

func TestServerCardNextStep(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{Name: "Support", Slug: "support"}
	setupURL := "https://dashboard.example.test/setup"
	added := &cardStatus{Label: "Added", Tone: cardToneDone}
	needsAction := &cardStatus{Label: "Needs action", Tone: cardToneAction}
	for _, tc := range []struct {
		nextAction string
		setupURL   string
		badge      *cardStatus
		status     *cardStatus
		nextStep   string
		action     *cardAction
	}{
		{nextActionStartSetup, "", added, nil, "Check that it is ready to use", nil},
		{"ready", "", added, nil, "Check that it is ready to use", nil},
		{nextActionContinueDashboardSetup, setupURL, needsAction, &cardStatus{Label: "Needs sign-in before anyone can use it", Tone: cardToneAction}, "Finish sign-in in the dashboard", &cardAction{Label: "Finish setup", URL: setupURL}},
		{nextActionSecureDashboardSetup, setupURL, needsAction, &cardStatus{Label: "Needs credentials before anyone can use it", Tone: cardToneAction}, "Enter its credentials in the dashboard", &cardAction{Label: "Finish setup", URL: setupURL}},
		{nextActionConnectSignIn, setupURL, needsAction, &cardStatus{Label: "Needs its sign-in connected before anyone can use it", Tone: cardToneAction}, "Connect its sign-in here in chat", &cardAction{Label: "Set up in dashboard", URL: setupURL, Secondary: true}},
		{nextActionConnectSignIn, "", needsAction, &cardStatus{Label: "Needs its sign-in connected before anyone can use it", Tone: cardToneAction}, "Connect its sign-in here in chat", nil},
		{nextActionContinueDashboardSetup, "", needsAction, &cardStatus{Label: "Needs sign-in before anyone can use it", Tone: cardToneAction}, "Finish setup from the MCP server's page in the dashboard", nil},
	} {
		card := serverCard("Example", project, tc.nextAction, tc.setupURL, false, "")
		require.Equal(t, "Example", card.Title)
		require.Equal(t, &cardStatus{Label: "Created", Tone: cardToneDone}, card.Pill)
		require.Equal(t, "MCP server · Support · private until added to a plugin", card.Meta)
		require.Equal(t, tc.badge, card.Badge, tc.nextAction)
		require.Equal(t, tc.status, card.Status, tc.nextAction)
		require.Equal(t, tc.action, card.Action, tc.nextAction)
		want := []cardDetail{{Label: "Next step", Text: tc.nextStep}}
		if tc.status != nil {
			want = append([]cardDetail{{Label: "Project", Text: "Support"}}, want...)
		}
		require.Equal(t, want, card.Details, tc.nextAction)
	}

	unnamed := serverCard("", ResolvedProject{Name: "", Slug: "support"}, "ready", "", false, "")
	require.Equal(t, "MCP server", unnamed.Title)
	require.Equal(t, "MCP server · support · private until added to a plugin", unnamed.Meta)
}

func TestServerCardShowsAlreadyAddedAndTheServerURL(t *testing.T) {
	t.Parallel()

	card := serverCard("Acme Tasks", ResolvedProject{Name: "Support", Slug: "support"}, "ready", "", true, "https://mcp.example.test/tasks")
	require.Equal(t, &cardStatus{Label: "Already added", Tone: cardToneNeutral}, card.Pill)
	require.Equal(t, cardDetail{Label: "Server URL", Copy: "https://mcp.example.test/tasks"}, card.Details[len(card.Details)-1])

	rendered := renderCard(t, card)
	require.Contains(t, rendered, `<span class="pill pill-neutral">Already added</span>`)
	require.Contains(t, rendered, `<code>https://mcp.example.test/tasks</code><button type="button" class="copy" data-copy="https://mcp.example.test/tasks">Copy</button>`)
	require.Contains(t, rendered, `<span class="badge badge-done" role="img" aria-label="Added">`)
	require.Contains(t, rendered, `<p class="line">MCP server · Support · private until added to a plugin</p>`)
	require.NotContains(t, rendered, "<a ", "a ready server has no next link")
}

// A card without rows is the compact variant: the header alone, no divider.
func TestCompactCardHasNoDetailRows(t *testing.T) {
	t.Parallel()

	rendered := renderCard(t, resultCard{Title: "Example", Badge: &cardStatus{Label: "Needs action", Tone: cardToneAction}, Meta: "MCP server", Action: &cardAction{Label: "View", URL: "https://dashboard.example.test/x", Secondary: true}})
	require.NotContains(t, rendered, "<dl")
	require.Contains(t, rendered, `<span class="badge badge-action" role="img" aria-label="Needs action">!</span>`)
	require.Contains(t, rendered, `<a class="button secondary" href="https://dashboard.example.test/x"`)
}

func TestSignInCardOpensTheAuthorizationURL(t *testing.T) {
	t.Parallel()

	authorizationURL := "https://dashboard.example.test/org/projects/p/mcp/example/inspect"
	card := signInCard("Acme Tasks", authorizationURL)
	require.Equal(t, "Sign in with Acme Tasks", card.Title)
	require.Equal(t, "AT", card.Initials)
	require.Equal(t, &cardStatus{Label: "Connect your Acme Tasks account to continue", Tone: cardTonePending}, card.Status)
	require.Equal(t, []cardDetail{{Label: "Requested by", Text: "Acme Tasks"}}, card.Details)

	rendered := renderCard(t, card)
	require.Contains(t, rendered, `<a class="button" href="`+authorizationURL+`"`)
	require.Contains(t, rendered, ">Sign in</a>")
	require.Equal(t, 1, strings.Count(rendered, "<a "), "the card offers one link")
	require.Contains(t, rendered, `<div class="tile tile-initials"><span aria-hidden="true">AT</span>`)
	require.Contains(t, rendered, `<span class="dot dot-pending" aria-hidden="true"></span>Connect your Acme Tasks account to continue`)

	unnamed := signInCard("", authorizationURL)
	require.Equal(t, "Sign in", unnamed.Title)
	require.Empty(t, unnamed.Initials)
	require.Nil(t, unnamed.Details)
	require.Equal(t, &cardAction{Label: "Sign in", URL: authorizationURL}, unnamed.Action)

	fallback := renderCard(t, serverCard("Example", ResolvedProject{Name: "Support", Slug: "support"}, nextActionConnectSignIn, authorizationURL, false, ""))
	require.Contains(t, fallback, `<a class="button secondary" href="`+authorizationURL+`"`)
}

func TestCardInitials(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{"Linear": "L", "acme tasks app": "AT", "  ": "", "<b> x": "X", "2fa Vault": "2V"} {
		require.Equal(t, want, cardInitials(name), name)
	}
}
