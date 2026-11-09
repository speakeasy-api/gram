package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Every tool on the external endpoint either links the card view or is listed
// as text-only with a reason, and every text-only entry names a real tool.
func TestEveryExternalToolHasACardOrAWrittenReason(t *testing.T) {
	t.Parallel()

	server, _ := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	advertised := map[string]bool{}
	for _, tool := range listAdvertisedTools(t, server) {
		advertised[tool.Name] = true
		if _, textOnly := textOnlyTools[tool.Name]; textOnly {
			require.Nil(t, tool.Meta["ui"], tool.Name)
			continue
		}
		require.Equal(t, map[string]any{"resourceUri": cardResourceURI}, tool.Meta["ui"], "tool %q has no card and no text-only reason", tool.Name)
	}
	for name, reason := range textOnlyTools {
		require.True(t, advertised[name], "text-only entry %q names no registered tool", name)
		require.NotEmpty(t, reason, name)
	}
}

func TestToolCardKind(t *testing.T) {
	t.Parallel()

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	cases := map[string]struct {
		annotations *mcp.ToolAnnotations
		want        cardKind
	}{
		"list_plugins":      {readOnly, cardKindList},
		"search_users":      {readOnly, cardKindList},
		"find_mcp":          {readOnly, cardKindList},
		"get_mcp":           {readOnly, cardKindDetail},
		"get_setup_handoff": {nil, cardKindDetail},
		"whoami":            {readOnly, cardKindDetail},
		"rename_plugin":     {nil, cardKindReceipt},
		"create_project":    {nil, cardKindReceipt},
	}
	for name, tc := range cases {
		kind, carded := toolCardKind(name, tc.annotations)
		require.True(t, carded, name)
		require.Equal(t, tc.want, kind, name)
	}
	_, carded := toolCardKind("run_analytics_query", readOnly)
	require.False(t, carded)
}

func genericCardHTML(t *testing.T, title string, kind cardKind, output any) string {
	t.Helper()

	result := withResultCard(title, kind, nil, output)
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
