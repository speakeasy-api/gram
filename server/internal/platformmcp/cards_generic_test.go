package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Every external tool is either in cardTools, linking the card view and a
// dashboard page, or in textOnlyTools with a reason, never both. The tool set
// joins the nil-service server with the live registrations the gallery
// reaches, so a tool registered only when its service is live is covered too.
func TestEveryExternalToolHasACardOrAWrittenReason(t *testing.T) {
	t.Parallel()

	server, _ := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	external := map[string]bool{}
	for _, tool := range listAdvertisedTools(t, server) {
		external[tool.Name] = true
		if _, textOnly := textOnlyTools[tool.Name]; textOnly {
			require.Nil(t, tool.Meta["ui"], tool.Name)
			continue
		}
		require.Equal(t, map[string]any{"resourceUri": cardResourceURI}, tool.Meta["ui"], "tool %q has no card and no text-only reason", tool.Name)
	}
	for _, descriptor := range galleryDescriptors() {
		if descriptor.Meta.servesAudience(AudienceExternal) {
			external[descriptor.Name] = true
		}
	}
	ownCard := map[string]bool{"whoami": true, "create_session_handoff_link": true}
	for name := range external {
		spec, carded := cardTools[name]
		reason, textOnly := textOnlyTools[name]
		require.NotEqual(t, carded, textOnly, "tool %q must be in exactly one of cardTools and textOnlyTools", name)
		if textOnly {
			require.NotEmpty(t, reason, name)
			continue
		}
		require.NotEmpty(t, spec.kind, name)
		if !ownCard[name] {
			require.NotEmpty(t, spec.route, "carded tool %q has no dashboard route", name)
			require.NotEmpty(t, spec.label, name)
		}
		for segment := range strings.SplitSeq(spec.route, "/") {
			if param, ok := strings.CutPrefix(segment, "{"); ok && segment != "{dashboard_path}" {
				_, known := cardRouteParams[strings.TrimSuffix(param, "}")]
				require.True(t, known, "tool %q route uses unknown segment %s", name, segment)
			}
		}
	}
	for name := range cardTools {
		require.True(t, external[name], "cardTools entry %q names no external tool", name)
	}
	for name := range textOnlyTools {
		require.True(t, external[name], "textOnlyTools entry %q names no external tool", name)
	}
}

func linkContext(org string) context.Context {
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{OrganizationSlug: org})
}

func TestCardLinksBuildDashboardPages(t *testing.T) {
	t.Parallel()

	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	links := cardLinks{dashboardURL: dashboard, serverURL: nil, projectSlug: func(_ context.Context, id string) string {
		if id == "p-9" {
			return "resolved"
		}
		return ""
	}}
	ctx := linkContext("acme")
	decode := func(raw string) jsonObject {
		decoded, err := decodeOrderedJSON([]byte(raw))
		require.NoError(t, err)
		object, _ := decoded.(jsonObject)
		return object
	}

	mcpLink := links.link(ctx, cardTools["get_mcp"], decode(`{"id":"m-1","slug":"crm","project_slug":"support","name":"CRM"}`), nil)
	require.NotNil(t, mcpLink)
	require.Equal(t, "https://app.example.test/acme/projects/support/mcp/crm", mcpLink.URL)
	require.Equal(t, "Open MCP server", mcpLink.Label)

	fromInput := links.link(ctx, cardTools["get_mcp_readiness"], decode(`{"state":"ready"}`), decode(`{"project_slug":"support","mcp_id":"m-1"}`))
	require.NotNil(t, fromInput)
	require.Equal(t, "https://app.example.test/acme/projects/support/mcp/m-1", fromInput.URL)

	plugin := links.link(ctx, cardTools["rename_plugin"], decode(`{"project_id":"p-1","plugin":{"id":"pl-1","name":"Sales"}}`), decode(`{"project_slug":"sales"}`))
	require.NotNil(t, plugin)
	require.Equal(t, "https://app.example.test/acme/projects/sales/plugins/pl-1", plugin.URL)

	byID := links.link(ctx, cardTools["list_plugins"], decode(`{"project_id":"p-9","plugins":[]}`), nil)
	require.NotNil(t, byID)
	require.Equal(t, "https://app.example.test/acme/projects/resolved/plugins", byID.URL)
	require.Nil(t, links.link(ctx, cardTools["list_plugins"], decode(`{"project_id":"p-unknown","plugins":[]}`), nil), "an unresolved project ID leaves no link")

	policy := links.link(ctx, cardTools["create_risk_policy"], decode(`{"project":{"id":"p-1","slug":"support"},"policy":{"id":"rp-1","enabled":true},"receipt":{"replayed":false}}`), nil)
	require.NotNil(t, policy)
	require.Equal(t, "https://app.example.test/acme/projects/support/risk-policies/rp-1", policy.URL)

	org := links.link(ctx, cardTools["list_access_roles"], decode(`{"roles":[]}`), nil)
	require.NotNil(t, org)
	require.Equal(t, "https://app.example.test/acme/access/roles", org.URL)

	board := links.link(ctx, cardTools["get_dashboard"], decode(`{"name":"Spend","dashboard_path":"/acme/projects/support/dashboards/d-1"}`), nil)
	require.NotNil(t, board)
	require.Equal(t, "https://app.example.test/acme/projects/support/dashboards/d-1", board.URL)

	// A page whose id cannot be filled falls back to its section's index.
	section := links.link(ctx, cardTools["get_plugin"], decode(`{"project_slug":"support","name":"Sales"}`), nil)
	require.NotNil(t, section)
	require.Equal(t, "https://app.example.test/acme/projects/support/plugins", section.URL)
	require.Equal(t, "Open plugins", section.Label)
	foreign := links.link(ctx, cardTools["get_dashboard"], decode(`{"project_slug":"support","dashboard_path":"/other/projects/support/dashboards/d-1"}`), nil)
	require.NotNil(t, foreign)
	require.Equal(t, "https://app.example.test/acme/projects/support/dashboards", foreign.URL, "a path for another organization is never used")

	// A missing project, organization or dashboard URL leaves no link rather
	// than a wrong one.
	require.Nil(t, links.link(ctx, cardTools["get_mcp"], decode(`{"name":"CRM","slug":"crm"}`), nil))
	require.Nil(t, links.link(context.Background(), cardTools["list_access_roles"], decode(`{}`), nil))
	require.Nil(t, cardLinks{}.link(ctx, cardTools["list_access_roles"], decode(`{}`), nil))
}

// A result's own next-step link wins over the page link.
func TestResultLinkWinsOverDashboardLink(t *testing.T) {
	t.Parallel()

	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	links := cardLinks{dashboardURL: dashboard, serverURL: nil, projectSlug: nil}
	result := withResultCard(linkContext("acme"), links, "Get MCP", cardTools["get_mcp"], nil, map[string]any{"name": "CRM", "slug": "crm", "project_slug": "support", "setup_url": "https://app.example.test/setup/1"}, nil)
	rendered, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, rendered, `href="https://app.example.test/setup/1"`)
	require.Contains(t, rendered, "Finish setup")

	result = withResultCard(linkContext("acme"), links, "Get MCP", cardTools["get_mcp"], nil, map[string]any{"name": "CRM", "slug": "crm", "project_slug": "support"}, nil)
	rendered, ok = result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, rendered, `href="https://app.example.test/acme/projects/support/mcp/crm"`)
	require.Contains(t, rendered, "Open MCP server")
}

func TestPlainTextRefusalKeepsItsMessage(t *testing.T) {
	t.Parallel()

	result := withResultCard(context.Background(), cardLinks{}, "Rename Plugin", cardTools["rename_plugin"], &mcp.CallToolResult{
		Meta:              nil,
		Content:           []mcp.Content{&mcp.TextContent{Text: "plugin_not_found: no plugin matched that ID"}},
		StructuredContent: nil,
		IsError:           true,
		InputRequests:     nil,
		RequestState:      "",
	}, nil, nil)
	rendered, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, rendered, "plugin_not_found: no plugin matched that ID")
}

func genericCardHTML(t *testing.T, title string, kind cardKind, output any) string {
	t.Helper()

	result := withResultCard(context.Background(), cardLinks{}, title, cardSpec{kind: kind, route: "", label: ""}, nil, output, nil)
	require.NotNil(t, result)
	rendered, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	return rendered
}

func TestListCardShowsCountRowsAndMore(t *testing.T) {
	t.Parallel()

	output := map[string]any{
		"project_slug": "support",
		"plugins": []map[string]any{
			{"name": "Support tools", "state": "published"},
			{"name": "Sales tools", "state": "draft"},
		},
		"next_cursor": "abc",
	}
	rendered := genericCardHTML(t, "List Plugins", cardKindList, output)
	require.Contains(t, rendered, "List Plugins")
	require.Contains(t, rendered, "2 plugins")
	require.Contains(t, rendered, "Support tools")
	require.Contains(t, rendered, "published")
	require.Contains(t, rendered, "<code>support</code>")
	require.Contains(t, rendered, "More results are available.")

	empty := genericCardHTML(t, "List Plugins", cardKindList, map[string]any{"plugins": []any{}})
	require.Contains(t, empty, "0 plugins")
	require.Contains(t, empty, "Nothing matched.")

	one := genericCardHTML(t, "List Plugins", cardKindList, map[string]any{"plugins": []map[string]any{{"name": "Only"}}})
	require.Contains(t, one, "1 plugin<")
	policy := genericCardHTML(t, "List Risk Policies", cardKindList, map[string]any{"policies": []map[string]any{{"name": "Only"}}})
	require.Contains(t, policy, "1 policy<")
}

func TestDetailCardFindsTheNamedSubject(t *testing.T) {
	t.Parallel()

	output := map[string]any{
		"snapshot_scope": "project",
		"policy":         map[string]any{"name": "Block secrets", "state": "enabled", "description": "Stops tool calls that leak secrets."},
		"project":        map[string]any{"slug": "support"},
	}
	rendered := genericCardHTML(t, "Get Risk Policy", cardKindDetail, output)
	require.Contains(t, rendered, "<h1>Block secrets</h1>")
	require.Contains(t, rendered, "enabled")
	require.Contains(t, rendered, "Stops tool calls that leak secrets.")
	require.Contains(t, rendered, "<code>support</code>")
}

// A read with no named subject but a list, such as a member's MCP servers,
// is drawn as that list.
func TestDetailWithoutSubjectFallsBackToList(t *testing.T) {
	t.Parallel()

	rendered := genericCardHTML(t, "See a Member's MCP Server Access", cardKindDetail, map[string]any{
		"member":  map[string]any{"masked_identity": ""},
		"servers": []map[string]any{{"mcp_name": "CRM"}, {"mcp_name": "Docs"}},
	})
	require.Contains(t, rendered, "2 servers")
	require.Contains(t, rendered, "CRM")
	require.NotContains(t, rendered, ">Details<")
}

func TestReceiptCardSaysWhatHappened(t *testing.T) {
	t.Parallel()

	done := genericCardHTML(t, "Rename Project", cardKindReceipt, map[string]any{"project": map[string]any{"name": "Support", "slug": "support"}, "receipt": map[string]any{"replayed": false}})
	require.Contains(t, done, ">Done<")
	require.Contains(t, done, "<h1>Support</h1>")
	require.Contains(t, done, "Rename Project")

	replayed := genericCardHTML(t, "Rename Project", cardKindReceipt, map[string]any{"project": map[string]any{"name": "Support"}, "receipt": map[string]any{"replayed": true}})
	require.Contains(t, replayed, ">No change<")

	preview := genericCardHTML(t, "Save Widget", cardKindReceipt, map[string]any{"preview": true, "widget": map[string]any{"name": "Spend"}})
	require.Contains(t, preview, "Preview, not applied")
}

func TestReceiptCardShowsAFailedTestEvent(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"refused", "not_found", "unreachable", "error"} {
		rendered := genericCardHTML(t, "Send a Data Export Test Event", cardKindReceipt, map[string]any{"outcome": outcome, "message": "The destination refused the test event."})
		require.NotContains(t, rendered, ">Done<", outcome)
		require.Contains(t, rendered, ">Not accepted<", outcome)
		require.Contains(t, rendered, "The destination refused the test event.", outcome)
	}

	accepted := genericCardHTML(t, "Send a Data Export Test Event", cardKindReceipt, map[string]any{"outcome": "accepted", "message": "The destination accepted the test event."})
	require.Contains(t, accepted, ">Done<")
}

func TestUnavailableResultDrawsAnUnavailableCard(t *testing.T) {
	t.Parallel()

	rendered := genericCardHTML(t, "List Skills", cardKindList, featureUnavailableResult{Code: unavailableCode, Feature: "skills", Message: "Skills are unavailable on this deployment."})
	require.Contains(t, rendered, ">Unavailable<")
	require.Contains(t, rendered, "Skills are unavailable on this deployment.")
}

// A card only shows values under named keys, so secrets elsewhere in a
// result never reach it, and only http(s) links become actions.
func TestGenericCardOmitsUnlistedFieldsAndUnsafeLinks(t *testing.T) {
	t.Parallel()

	output := map[string]any{
		"name":          `<script>alert(1)</script>`,
		"client_secret": "s3cr3t-value",
		"access_token":  "tok-value",
		"upstream_url":  "https://upstream.example.test/private",
		"url":           "javascript:alert(2)",
	}
	rendered := genericCardHTML(t, "Get MCP", cardKindDetail, output)
	require.NotContains(t, rendered, "s3cr3t-value")
	require.NotContains(t, rendered, "tok-value")
	require.NotContains(t, rendered, "upstream.example.test")
	require.NotContains(t, rendered, "javascript:")
	require.NotContains(t, rendered, "<script")
	require.Contains(t, rendered, "&lt;script&gt;")

	linked := genericCardHTML(t, "Create Session Handoff Link", cardKindReceipt, map[string]any{"url": "https://app.example.test/handoff/1"})
	require.Contains(t, linked, `href="https://app.example.test/handoff/1"`)
}

// The hook adds a card in _meta and leaves the text and structured content
// every other client reads unchanged; a text-only tool gets none.
func TestAddToolAttachesCardWithoutChangingTheResult(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	registrar := newRegistrar(server)
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})
	type output struct {
		Plugins []map[string]string `json:"plugins"`
	}
	want := output{Plugins: []map[string]string{{"name": "Support tools"}}}
	handler := func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, output, error) {
		return nil, want, nil
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone}
	addTool(registrar, &mcp.Tool{Name: "list_plugins", Title: "List Plugins", Annotations: readOnlyAnnotations()}, meta, handler)
	addTool(registrar, &mcp.Tool{Name: "run_analytics_query", Annotations: readOnlyAnnotations()}, meta, handler)
	session := connectTestClient(t, server)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_plugins", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	card, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, card, "1 plugin<")
	encoded, err := json.Marshal(want)
	require.NoError(t, err)
	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.JSONEq(t, string(encoded), string(structured))
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, string(encoded), text.Text)

	plain, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "run_analytics_query", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.Nil(t, plain.Meta[cardMetaKey])
}

// A denied call keeps its refusal payload and shows a readable refusal card
// with the request-access link.
func TestDeniedCallDrawsARefusalCard(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	registrar := newRegistrar(server)
	registrar.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{
		RequiredScope:    "org:admin",
		RequestAccessURL: "https://app.example.test/example/request-access",
		cause:            ErrForbidden,
	}})
	addTool(registrar, &mcp.Tool{Name: "rename_plugin", Title: "Rename Plugin"}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, nil
	})
	session := connectTestClient(t, server)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "rename_plugin", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"permission_denied"`)
	card, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, card, "Not completed")
	require.Contains(t, card, "This action requires org:admin.")
	require.Contains(t, card, `href="https://app.example.test/example/request-access"`)
}
