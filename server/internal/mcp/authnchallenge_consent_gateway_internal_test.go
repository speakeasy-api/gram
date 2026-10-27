package mcp

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
)

func gatewayTestMember(slug, name string, issuer uuid.UUID, projectID uuid.UUID) metaMember {
	return metaMember{
		slug:                  slug,
		name:                  name,
		projectID:             projectID,
		remoteSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: issuer != uuid.Nil},
	}
}

func gatewayTestCard(clientID string, issuer uuid.UUID, projectID uuid.NullUUID) remoteSessionCard {
	return remoteSessionCard{ClientID: clientID, IssuerDisplay: clientID, remoteSessionIssuerID: issuer, projectID: projectID}
}

func serverBySlug(t *testing.T, view *gatewayConsentView, slug string) gatewayServer {
	t.Helper()
	for _, server := range view.Servers {
		if server.Slug == slug {
			return server
		}
	}
	t.Fatalf("server %q not listed", slug)
	return gatewayServer{}
}

// Each server takes its status from the services that sign in to it: none
// needed, connected, needing a reconnect, or not connected. Servers needing
// action list first, and only usable ones count as connected.
func TestGatewayServersView_StatusesAndOrder(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	github, linear, notion := uuid.New(), uuid.New(), uuid.New()
	members := []metaMember{
		gatewayTestMember("docs", "Docs", uuid.Nil, project),
		gatewayTestMember("github", "GitHub", github, project),
		gatewayTestMember("linear", "Linear", linear, project),
		gatewayTestMember("notion", "Notion", notion, project),
	}
	githubCard := gatewayTestCard("github-client", github, uuid.NullUUID{UUID: project, Valid: true})
	githubCard.Connected = true
	linearCard := gatewayTestCard("linear-client", linear, uuid.NullUUID{UUID: project, Valid: true})
	linearCard.Expired = true
	notionCard := gatewayTestCard("notion-client", notion, uuid.NullUUID{})

	view := gatewayServersView(members, []remoteSessionCard{githubCard, linearCard, notionCard})

	require.Equal(t, gatewayServerNoSignIn, serverBySlug(t, view, "docs").Status)
	require.Equal(t, gatewayServerConnected, serverBySlug(t, view, "github").Status)
	require.Equal(t, gatewayServerReconnect, serverBySlug(t, view, "linear").Status)
	require.Equal(t, gatewayServerNotConnected, serverBySlug(t, view, "notion").Status)
	require.Equal(t, 2, view.ReadyCount)

	order := make([]string, 0, len(view.Servers))
	for _, server := range view.Servers {
		order = append(order, server.Slug)
	}
	require.Equal(t, []string{"linear", "notion", "github", "docs"}, order)
}

// A server's connect page for an upstream no linked service signs in to says
// an administrator must set it up, rather than offering nothing to connect.
func TestConsentTemplate_GatewayServerNotSetUp(t *testing.T) {
	t.Parallel()

	view := gatewayServersView([]metaMember{gatewayTestMember("eng.jira", "Jira", uuid.New(), uuid.New())}, nil)
	require.Equal(t, gatewayServerNotSetUp, view.Servers[0].Status)
	require.Equal(t, 0, view.ReadyCount)
	focus := view.Servers[0]
	view.Focus = &focus

	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))
	require.Contains(t, page, "Ask an administrator to set up sign-in for it")
	require.NotContains(t, page, "Connecting is all")
}

// A project's service signs in only that project's servers; a service no
// project owns signs in any server on its authorization server. A server no
// service signs in to cannot be connected until one is set up.
func TestGatewayServersView_MatchesCardsByProject(t *testing.T) {
	t.Parallel()

	first, second := uuid.New(), uuid.New()
	github := uuid.New()
	members := []metaMember{
		gatewayTestMember("first.github", "GitHub", github, first),
		gatewayTestMember("second.github", "GitHub", github, second),
	}
	firstCard := gatewayTestCard("first-client", github, uuid.NullUUID{UUID: first, Valid: true})
	firstCard.Connected = true

	view := gatewayServersView(members, []remoteSessionCard{firstCard})
	require.Equal(t, gatewayServerConnected, serverBySlug(t, view, "first.github").Status)
	require.Equal(t, gatewayServerNotSetUp, serverBySlug(t, view, "second.github").Status)

	orgCard := gatewayTestCard("org-client", github, uuid.NullUUID{})
	view = gatewayServersView(members, []remoteSessionCard{orgCard})
	require.Equal(t, gatewayServerNotConnected, serverBySlug(t, view, "first.github").Status)
	require.Equal(t, gatewayServerNotConnected, serverBySlug(t, view, "second.github").Status)
}

// A service the identity provider signs in to counts once its exchange is
// known to work; before that the server is not counted as connected.
func TestGatewayServersView_ChainedCountsOnceChecked(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	idp := uuid.New()
	members := []metaMember{gatewayTestMember("wiki", "Wiki", idp, project)}
	card := gatewayTestCard("wiki-client", idp, uuid.NullUUID{})
	card.Chained = true
	card.ChainCheck = chainCheckPending

	view := gatewayServersView(members, []remoteSessionCard{card})
	require.Equal(t, gatewayServerChainPending, view.Servers[0].Status)
	require.Equal(t, 0, view.ReadyCount)

	card.ChainCheck = chainCheckConnected
	view = gatewayServersView(members, []remoteSessionCard{card})
	require.Equal(t, gatewayServerChained, view.Servers[0].Status)
	require.Equal(t, 1, view.ReadyCount)
}

// One usable service is enough: a server routes its calls to whichever of its
// services holds a credential.
func TestGatewayServersView_AnyUsableServiceConnects(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	github := uuid.New()
	members := []metaMember{gatewayTestMember("github", "GitHub", github, project)}
	stale := gatewayTestCard("project-client", github, uuid.NullUUID{UUID: project, Valid: true})
	stale.Expired = true
	live := gatewayTestCard("org-client", github, uuid.NullUUID{})
	live.Connected = true

	view := gatewayServersView(members, []remoteSessionCard{stale, live})
	require.Equal(t, gatewayServerConnected, serverBySlug(t, view, "github").Status)
	require.Equal(t, 1, view.ReadyCount)
}

// A server's connect page shows only its services, and says how many other
// servers each one also connects.
func TestFocusedCards_NarrowsAndNotesSharedServices(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	github, linear := uuid.New(), uuid.New()
	members := []metaMember{
		gatewayTestMember("github", "GitHub", github, project),
		gatewayTestMember("github-enterprise", "GitHub Enterprise", github, project),
		gatewayTestMember("linear", "Linear", linear, project),
	}
	cards := []remoteSessionCard{
		gatewayTestCard("github-client", github, uuid.NullUUID{}),
		gatewayTestCard("linear-client", linear, uuid.NullUUID{}),
	}
	view := gatewayServersView(members, cards)

	focused := focusedCards(view, serverBySlug(t, view, "github"), cards)
	require.Len(t, focused, 1)
	require.Equal(t, "github-client", focused[0].ClientID)
	require.Equal(t, "Also connects 1 other server on this gateway", focused[0].SharedServerNote)

	focused = focusedCards(view, serverBySlug(t, view, "linear"), cards)
	require.Len(t, focused, 1)
	require.Empty(t, focused[0].SharedServerNote)
}

func TestGatewayServerConnectURL(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"https://app.example.com/mcp/gateway/acme/connect/first-party?server=eng.github",
		gatewayServerConnectURL("https://app.example.com/mcp/gateway/acme", "eng.github"))
	require.Equal(t,
		"https://app.example.com/mcp/tools/connect/first-party?server=a+b%26c",
		gatewayServerConnectURL("https://app.example.com/mcp/tools", "a b&c"))
	require.Empty(t, gatewayServerConnectURL("", "github"))
	require.Empty(t, gatewayServerConnectURL("https://app.example.com/mcp/tools", ""))
}

// Only a member that signs in through a service on a gateway a person signs
// in to has a connect page to point at.
func TestMetaGateMemberConnectURL(t *testing.T) {
	t.Parallel()

	issuer := uuid.New()
	gate := metaGateContext{connectRoot: "https://app.example.com/mcp/tools"}
	require.Equal(t, "https://app.example.com/mcp/tools/connect/first-party?server=github", gate.memberConnectURL(gatewayTestMember("github", "", issuer, uuid.New())))
	require.Empty(t, gate.memberConnectURL(gatewayTestMember("docs", "", uuid.Nil, uuid.New())))
	require.Empty(t, metaGateContext{}.memberConnectURL(gatewayTestMember("github", "", issuer, uuid.New())))
}

// A member with nothing routed, or a rejected credential, names the page that
// connects it when the gateway has one.
func TestMemberAuthFailure_LinksConnectPage(t *testing.T) {
	t.Parallel()

	member := gatewayTestMember("github", "GitHub", uuid.New(), uuid.New())
	connectURL := "https://app.example.com/mcp/tools/connect/first-party?server=github"

	err := memberAuthFailure(member, memberDial{anonymous: true, connectURL: connectURL})
	require.ErrorContains(t, err, `server "github" is not connected for this user`)
	require.ErrorContains(t, err, connectURL)

	err = memberAuthFailure(member, memberDial{anonymous: false, connectURL: connectURL})
	require.ErrorContains(t, err, "rejected the user's credential")
	require.ErrorContains(t, err, connectURL)

	err = memberAuthFailure(member, memberDial{anonymous: true})
	require.ErrorContains(t, err, "connect it from this gateway's sign-in page")
}

func renderGatewayConsent(t *testing.T, data consentTemplateData) string {
	t.Helper()
	var page bytes.Buffer
	require.NoError(t, consentTemplate.Execute(&page, data))
	return normalizeWhitespace(page.String())
}

func gatewayTemplateData(view *gatewayConsentView, cards []remoteSessionCard) consentTemplateData {
	return consentTemplateData{
		ClientName:         "Example Client",
		MCPSlug:            "acme",
		MCPRouteBase:       mcproute.OrganizationGateway,
		State:              "state-id",
		CSRFToken:          "csrf",
		SubjectDisplay:     "user@example.com",
		ScriptURL:          "/mcp/consent-page-test.js",
		RemoteSessionCards: cards,
		ConsentEnabled:     true,
		Gateway:            view,
	}
}

// A gateway's page approves the gateway: it summarizes its servers behind a
// disclosure, links each to its connect page, and lists no services.
func TestConsentTemplate_GatewayListsServers(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	github := uuid.New()
	view := gatewayServersView([]metaMember{
		gatewayTestMember("docs", "Docs", uuid.Nil, project),
		gatewayTestMember("eng.github", "GitHub", github, project),
	}, []remoteSessionCard{gatewayTestCard("github-client", github, uuid.NullUUID{})})

	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))

	require.Contains(t, page, "1 of 2 servers connected")
	require.Contains(t, page, "Show servers")
	// A server signing in through one service connects in place, with no
	// stop on its own page; a server needing no sign-in has nothing to do.
	require.Contains(t, page, `<input type="hidden" name="client_id" value="github-client" />`)
	require.Contains(t, page, "data-connect-link > Connect </button>")
	require.NotContains(t, page, "data-gateway-server-page")
	require.Contains(t, page, `id="consent-approve-button"`)
	require.NotContains(t, page, "data-service-connections")
	require.NotContains(t, page, "Connect a service above to enable access")
}

// A server's connect page shows its services with no approval: connecting is
// what makes the server usable, and the page says so.
func TestConsentTemplate_GatewayServerPageHasNoApproval(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	github := uuid.New()
	cards := []remoteSessionCard{gatewayTestCard("github-client", github, uuid.NullUUID{})}
	view := gatewayServersView([]metaMember{gatewayTestMember("eng.github", "GitHub", github, project)}, cards)
	focus := view.Servers[0]
	view.Focus = &focus

	page := renderGatewayConsent(t, gatewayTemplateData(view, focusedCards(view, focus, cards)))

	require.Contains(t, page, "Connect a server")
	require.Contains(t, page, "Connecting is all GitHub needs")
	require.Contains(t, page, "Go back to all servers to finish giving Example Client access")
	require.Contains(t, page, `<input type="hidden" name="server" value="eng.github" />`)
	require.Contains(t, page, `/mcp/gateway/acme/connect?state=state-id&server=" class="bg-card`)
	require.Contains(t, page, "data-service-connections")
	require.NotContains(t, page, "consent-approve-button")

	firstParty := gatewayTemplateData(view, focusedCards(view, focus, cards))
	firstParty.FirstParty = true
	page = renderGatewayConsent(t, firstParty)
	require.Contains(t, page, "Go back to your AI client and try again")
	require.NotContains(t, page, "consent-approve-button")
}

// A server the identity provider is still being checked for is a sign-in
// server: its page must not claim it needs no sign-in beside the pending
// identity-provider card.
func TestConsentTemplate_GatewayChainPendingServerPage(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	idp := uuid.New()
	card := gatewayTestCard("wiki-client", idp, uuid.NullUUID{})
	card.Chained = true
	card.ChainCheck = chainCheckPending
	cards := []remoteSessionCard{card}
	view := gatewayServersView([]metaMember{gatewayTestMember("wiki", "Wiki", idp, project)}, cards)
	require.Equal(t, gatewayServerChainPending, view.Servers[0].Status)
	require.True(t, view.Servers[0].NeedsSignIn())

	list := renderGatewayConsent(t, gatewayTemplateData(view, nil))
	require.Contains(t, list, `server=wiki" class="text-muted-foreground interact:text-foreground shrink-0 text-xs" data-gateway-server-page >Connect`)

	focus := view.Servers[0]
	view.Focus = &focus
	page := renderGatewayConsent(t, gatewayTemplateData(view, focusedCards(view, focus, cards)))
	require.Contains(t, page, "Connecting is all Wiki needs")
	require.NotContains(t, page, "needs no sign-in")
}

// A server signing in through several services connects on its own page,
// where each is named; one with a usable connection is managed there.
func TestConsentTemplate_GatewayListLinksServerPages(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	github, linear := uuid.New(), uuid.New()
	projectCard := gatewayTestCard("project-client", github, uuid.NullUUID{UUID: project, Valid: true})
	orgCard := gatewayTestCard("org-client", github, uuid.NullUUID{})
	linearCard := gatewayTestCard("linear-client", linear, uuid.NullUUID{})
	linearCard.Connected = true
	view := gatewayServersView([]metaMember{
		gatewayTestMember("eng.github", "GitHub", github, project),
		gatewayTestMember("eng.linear", "Linear", linear, project),
	}, []remoteSessionCard{projectCard, orgCard, linearCard})
	require.Empty(t, serverBySlug(t, view, "eng.github").ConnectClientID)

	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))
	require.Contains(t, page, `/mcp/gateway/acme/connect?state=state-id&server=eng.github" class="text-muted-foreground interact:text-foreground shrink-0 text-xs" data-gateway-server-page >Connect`)
	require.Contains(t, page, `/mcp/gateway/acme/connect?state=state-id&server=eng.linear" class="text-muted-foreground interact:text-foreground shrink-0 text-xs" data-gateway-server-page >Manage`)
	require.NotContains(t, page, "data-connect-link")
}

// The list renders open or collapsed as the view decides.
func TestConsentTemplate_GatewayListOpenState(t *testing.T) {
	t.Parallel()

	view := gatewayServersView([]metaMember{gatewayTestMember("docs", "Docs", uuid.Nil, uuid.New())}, nil)
	view.ListOpen = true
	require.Contains(t, renderGatewayConsent(t, gatewayTemplateData(view, nil)), `data-state="state-id" open`)
	view.ListOpen = false
	require.NotContains(t, renderGatewayConsent(t, gatewayTemplateData(view, nil)), `data-state="state-id" open`)
}

// A server's status does not depend on the order of its services: a pending
// identity-provider sign-in outranks a lapsed connection either way round.
func TestGatewayServersView_StatusIgnoresServiceOrder(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	idp := uuid.New()
	members := []metaMember{gatewayTestMember("wiki", "Wiki", idp, project)}
	pending := gatewayTestCard("chained-client", idp, uuid.NullUUID{})
	pending.Chained = true
	pending.ChainCheck = chainCheckPending
	expired := gatewayTestCard("expired-client", idp, uuid.NullUUID{UUID: project, Valid: true})
	expired.Expired = true

	require.Equal(t, gatewayServerChainPending, gatewayServersView(members, []remoteSessionCard{pending, expired}).Servers[0].Status)
	require.Equal(t, gatewayServerChainPending, gatewayServersView(members, []remoteSessionCard{expired, pending}).Servers[0].Status)
}

// A server whose sign-in is not set up links to its page, which says what an
// administrator must do; one needing no sign-in has nothing to open.
func TestConsentTemplate_GatewayListLinksNotSetUpServer(t *testing.T) {
	t.Parallel()

	view := gatewayServersView([]metaMember{
		gatewayTestMember("eng.jira", "Jira", uuid.New(), uuid.New()),
		gatewayTestMember("docs", "Docs", uuid.Nil, uuid.New()),
	}, nil)
	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))
	require.Contains(t, page, `server=eng.jira" class="text-muted-foreground interact:text-foreground shrink-0 text-xs" data-gateway-server-page >View`)
	require.NotContains(t, page, "server=docs")
}
