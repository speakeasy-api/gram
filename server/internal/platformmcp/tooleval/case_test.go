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

// LoadCases rejects duplicate ids and cases without a prompt, so loading
// every embedded file is the check.
func TestLoadCasesParsesEveryFile(t *testing.T) {
	t.Parallel()

	cases, err := LoadCases()
	require.NoError(t, err)
	require.NotEmpty(t, cases)
}
