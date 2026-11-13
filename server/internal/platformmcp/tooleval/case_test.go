package tooleval

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArgsMatch(t *testing.T) {
	t.Parallel()

	arguments := json.RawMessage(`{"query":"Linear issues","project_slug":"payments","limit":5,"names":["a","b"],"enabled":false}`)
	require.True(t, argsMatch(nil, arguments))
	require.True(t, argsMatch(map[string]any{"query": "linear"}, arguments), "strings match by case-insensitive substring")
	require.True(t, argsMatch(map[string]any{"limit": 5, "names": []any{"a", "b"}, "enabled": false}, arguments), "other values match as JSON")
	require.False(t, argsMatch(map[string]any{"project_slug": "default"}, arguments))
	require.False(t, argsMatch(map[string]any{"limit": 6}, arguments))
	require.False(t, argsMatch(map[string]any{"missing": "x"}, arguments))
	require.False(t, argsMatch(map[string]any{"limit": "5"}, arguments), "a string never matches a number")
	require.False(t, argsMatch(map[string]any{"query": "x"}, json.RawMessage(`not json`)))

	urns := json.RawMessage(`{"ids":["urn:tool:refund_invoice","urn:tool:void_invoice"]}`)
	require.True(t, argsMatch(map[string]any{"ids": "refund_invoice"}, urns), "a string matches any array element")
	require.True(t, argsMatch(map[string]any{"ids": []any{"void_invoice", "refund_invoice"}}, urns), "a list matches in any order")
	require.False(t, argsMatch(map[string]any{"ids": []any{"refund_invoice", "send_invoice"}}, urns), "every listed string must be present")

	nested := json.RawMessage(`{"operations":[{"op":"move_card","placement_id":"p1"},{"op":"add_card","widget_id":"w-1","w":12}],"filters":{"range":{"preset":"30d","label":"Last 30 days"}}}`)
	require.True(t, argsMatch(map[string]any{"operations": []any{map[string]any{"op": "add_card", "widget_id": "w-1"}}}, nested), "a listed object matches some element on the keys it lists")
	require.True(t, argsMatch(map[string]any{"filters": map[string]any{"range": map[string]any{"preset": "30d"}}}, nested), "objects match on the keys they list")
	require.False(t, argsMatch(map[string]any{"operations": []any{map[string]any{"op": "remove_card"}}}, nested))
	require.False(t, argsMatch(map[string]any{"filters": map[string]any{"range": map[string]any{"preset": "7d"}}}, nested))
}

func TestCaseSeparatesOrientationFromFirstSteps(t *testing.T) {
	t.Parallel()

	c := Case{ID: "c", Prompt: "p", Expect: []string{"list_projects"}, Allow: []string{"get_mcp"}, Args: nil, File: ""}
	require.True(t, c.orients("whoami"))
	require.False(t, c.orients("list_projects"), "an expected tool is the target even when it is an orientation read")
	require.False(t, c.orients("get_mcp"), "an allowed first step ends the attempt")
	require.True(t, c.onPath("get_mcp"))
	require.False(t, c.onPath("disable_mcp"))

	allowsOrientation := Case{ID: "d", Prompt: "p", Expect: []string{"disable_mcp"}, Allow: []string{"find_mcp"}, Args: nil, File: ""}
	require.False(t, allowsOrientation.orients("find_mcp"), "an orientation read listed in allow ends the attempt")
	require.True(t, allowsOrientation.onPath("find_mcp"))
}

func TestAttachIdentityProviderCaseAcceptsReadinessFirst(t *testing.T) {
	t.Parallel()

	cases, err := LoadCases()
	require.NoError(t, err)
	var c Case
	for _, candidate := range cases {
		if candidate.ID == "mcp-attach-idp-linear" {
			c = candidate
			break
		}
	}
	require.Equal(t, "mcp-attach-idp-linear", c.ID)
	require.Equal(t, []string{"attach_platform_mcp_identity_provider"}, c.Expect)
	require.Equal(t, []string{"find_mcp", "get_mcp", "inspect_mcp_candidate", "get_mcp_connection_settings", "get_mcp_readiness"}, c.Allow)

	for _, tc := range []struct {
		tool   string
		hit    bool
		onPath bool
	}{
		{tool: "get_mcp_readiness", hit: false, onPath: true},
		{tool: "attach_platform_mcp_identity_provider", hit: true, onPath: false},
		{tool: "get_mcp_network_traffic", hit: false, onPath: false},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			require.False(t, c.orients(tc.tool), "each call ends the attempt, rather than becoming an orientation read")
			o := Outcome{Case: c, Selected: &Call{Tool: tc.tool, Arguments: json.RawMessage(`{}`)}}
			score(&o, nil, nil)
			require.Equal(t, tc.hit, o.Hit)
			require.Equal(t, tc.onPath, o.OnPath)
		})
	}
}

// LoadCases rejects duplicate ids and cases without a prompt, so loading
// every embedded file is the check.
func TestLoadCasesParsesEveryFile(t *testing.T) {
	t.Parallel()

	cases, err := LoadCases()
	require.NoError(t, err)
	require.NotEmpty(t, cases)
}
