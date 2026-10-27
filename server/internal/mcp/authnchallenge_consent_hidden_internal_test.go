package mcp

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func hiddenTestMember(slug, name string, issuer uuid.UUID, project uuid.UUID) metaMember {
	member := gatewayTestMember(slug, name, issuer, project)
	member.serverID = uuid.New()
	return member
}

func serverSlugs(servers []gatewayServer) []string {
	slugs := make([]string, 0, len(servers))
	for _, server := range servers {
		slugs = append(slugs, server.Slug)
	}
	return slugs
}

func hiddenTestView() (*gatewayConsentView, map[string]metaMember) {
	project := uuid.New()
	github, linear := uuid.New(), uuid.New()
	members := []metaMember{
		hiddenTestMember("docs", "Docs", uuid.Nil, project),
		hiddenTestMember("github", "GitHub", github, project),
		hiddenTestMember("linear", "Linear", linear, project),
	}
	githubCard := gatewayTestCard("github-client", github, uuid.NullUUID{UUID: project, Valid: true})
	githubCard.Connected = true
	linearCard := gatewayTestCard("linear-client", linear, uuid.NullUUID{UUID: project, Valid: true})
	bySlug := make(map[string]metaMember, len(members))
	for _, member := range members {
		bySlug[member.slug] = member
	}
	view := gatewayServersView(members, []remoteSessionCard{githubCard, linearCard})
	view.CanHide = true
	return view, bySlug
}

func TestGatewayConsentView_MarkHiddenSortsLastAndUncounts(t *testing.T) {
	t.Parallel()

	view, members := hiddenTestView()
	require.Equal(t, 2, view.ReadyCount)

	view.markHidden(map[uuid.UUID]bool{members["docs"].serverID: true, members["linear"].serverID: true})

	require.Equal(t, 1, view.ReadyCount)
	require.Equal(t, []string{"github", "linear", "docs"}, serverSlugs(view.Servers))
	require.Equal(t, []string{"github"}, serverSlugs(view.VisibleServers()))
	require.Equal(t, []string{"linear", "docs"}, serverSlugs(view.HiddenServers()))
	require.Len(t, view.Servers, 3, "the accessors must not mutate the view")
	require.Equal(t, members["docs"].serverID.String(), serverBySlug(t, view, "docs").ServerID)
}

func TestGatewayConsentView_MarkHiddenNoneIsNoop(t *testing.T) {
	t.Parallel()

	view, _ := hiddenTestView()
	view.markHidden(map[uuid.UUID]bool{uuid.New(): true})

	require.Equal(t, 2, view.ReadyCount)
	require.Empty(t, view.HiddenServers())
}

func TestGatewayConsentView_ApplyOutcomesSkipsHidden(t *testing.T) {
	t.Parallel()

	view, members := hiddenTestView()
	view.markHidden(map[uuid.UUID]bool{members["github"].serverID: true})
	view.applyOutcomes([]consentMemberOutcome{
		{Slug: "docs", Name: "Docs", Status: gatewayServerNoSignIn, Label: ""},
		{Slug: "github", Name: "GitHub", Status: gatewayServerConnected, Label: ""},
		{Slug: "linear", Name: "Linear", Status: gatewayServerConnected, Label: ""},
	})

	require.Equal(t, 2, view.ReadyCount)
	require.Equal(t, "github", view.Servers[len(view.Servers)-1].Slug)
}

func TestConsentTemplate_GatewayHiddenServersListedSeparately(t *testing.T) {
	t.Parallel()

	view, members := hiddenTestView()
	view.markHidden(map[uuid.UUID]bool{members["docs"].serverID: true})

	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))

	require.Contains(t, page, "1 of 2 servers connected · 1 hidden")
	visibleStart := strings.Index(page, "data-gateway-visible-list")
	hiddenStart := strings.Index(page, "data-gateway-hidden-list")
	require.Positive(t, visibleStart)
	require.Greater(t, hiddenStart, visibleStart)
	visible, hidden := page[visibleStart:hiddenStart], page[hiddenStart:]

	docsInput := `name="server_id" value="` + members["docs"].serverID.String() + `"`
	require.NotContains(t, visible, docsInput)
	require.Contains(t, hidden, docsInput)
	require.Contains(t, hidden, `value="show_server"`)
	require.NotContains(t, hidden, `value="hide_server"`)

	for _, slug := range []string{"github", "linear"} {
		require.Contains(t, visible, `name="server_id" value="`+members[slug].serverID.String()+`"`)
	}
	require.Contains(t, visible, `value="hide_server"`)
	require.NotContains(t, visible, `value="show_server"`)
	require.Contains(t, page, "data-gateway-all-hidden hidden")
	require.NotContains(t, page, "data-gateway-hidden-section hidden")
	require.Contains(t, page, `<input type="hidden" name="csrf_token" value="csrf" />`)
}

func TestConsentTemplate_GatewayAllServersHidden(t *testing.T) {
	t.Parallel()

	view, members := hiddenTestView()
	hidden := make(map[uuid.UUID]bool, len(members))
	for _, member := range members {
		hidden[member.serverID] = true
	}
	view.markHidden(hidden)

	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))

	require.Contains(t, page, "0 of 0 servers connected · 3 hidden")
	require.Contains(t, page, "data-gateway-all-hidden > You've hidden every server on this gateway.")
	require.Contains(t, page, "data-gateway-visible-list hidden")
	require.NotContains(t, page, `value="hide_server"`)
}

func TestConsentTemplate_GatewayNoHiddenServers(t *testing.T) {
	t.Parallel()

	view, _ := hiddenTestView()
	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))

	require.Contains(t, page, "2 of 3 servers connected</span")
	require.Contains(t, page, "data-gateway-hidden-section hidden")
	require.Contains(t, page, "data-gateway-all-hidden hidden")
	require.NotContains(t, page, `value="show_server"`)
}

func TestGatewayConsentView_AnyReadyCountsHiddenServers(t *testing.T) {
	t.Parallel()

	view, members := hiddenTestView()
	view.markHidden(map[uuid.UUID]bool{members["docs"].serverID: true, members["github"].serverID: true})

	require.Equal(t, 0, view.ReadyCount)
	require.True(t, view.AnyReady(), "hiding every ready server must not block approval")

	view.applyOutcomes([]consentMemberOutcome{
		{Slug: "docs", Name: "Docs", Status: gatewayServerNoSignIn, Label: ""},
		{Slug: "github", Name: "GitHub", Status: gatewayServerConnected, Label: ""},
		{Slug: "linear", Name: "Linear", Status: gatewayServerNotConnected, Label: ""},
	})
	require.Equal(t, 0, view.ReadyCount)
	require.True(t, view.AnyReady())

	view.applyOutcomes([]consentMemberOutcome{{Slug: "linear", Name: "Linear", Status: gatewayServerNotConnected, Label: ""}})
	require.False(t, view.AnyReady())
}

func TestGatewayServersView_ReadyCountIsVisibleReadyOnly(t *testing.T) {
	t.Parallel()

	view, members := hiddenTestView()
	require.Equal(t, 2, view.ReadyCount)
	view.markHidden(map[uuid.UUID]bool{members["github"].serverID: true})
	require.Equal(t, 1, view.ReadyCount)
	// Re-marking with a different set clears the earlier mark.
	view.markHidden(map[uuid.UUID]bool{members["linear"].serverID: true})
	require.Equal(t, 2, view.ReadyCount)
	require.Equal(t, []string{"linear"}, serverSlugs(view.HiddenServers()))
}

func TestConsentTemplate_GatewayVisibilityFormsNeedCanHide(t *testing.T) {
	t.Parallel()

	view, _ := hiddenTestView()
	require.Contains(t, renderGatewayConsent(t, gatewayTemplateData(view, nil)), "data-gateway-visibility-form")

	view.CanHide = false
	page := renderGatewayConsent(t, gatewayTemplateData(view, nil))
	require.NotContains(t, page, "data-gateway-visibility-form")
	require.NotContains(t, page, `value="hide_server"`)
}

func TestIsAssistantRequest(t *testing.T) {
	t.Parallel()

	require.False(t, isAssistantRequest(t.Context()))
	ctx := contextvalues.SetAssistantPrincipal(t.Context(), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	require.True(t, isAssistantRequest(ctx))
}
