package tooleval

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func selectFixture() (Catalogue, []Case) {
	catalogue := Catalogue{
		Instructions: map[string]string{"direct": "full", "progressive": "small"},
		Tools: []Tool{
			{Name: "get_mcp", Description: "Read one server.", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: true},
			{Name: "disable_mcp", Description: "Turn a server off.", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: false},
			{Name: "list_skills", Description: "List skills.", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: true},
		},
	}
	cases := []Case{
		{ID: "read", Prompt: "Show me GitHub.", Expect: []string{"get_mcp"}, Allow: nil, Args: nil, File: "a.yaml"},
		{ID: "disable", Prompt: "Turn Linear off.", Expect: []string{"disable_mcp"}, Allow: []string{"get_mcp"}, Args: nil, File: "a.yaml"},
		{ID: "skills", Prompt: "Which skills exist?", Expect: []string{"list_skills"}, Allow: nil, Args: nil, File: "b.yaml"},
		{ID: "weather", Prompt: "Weather?", Expect: nil, Allow: nil, Args: nil, File: "n.yaml"},
	}
	return catalogue, cases
}

func caseIDs(cases []Case) []string {
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestSelectChangedRunsNothingWithoutAChange(t *testing.T) {
	t.Parallel()

	catalogue, cases := selectFixture()
	// Reindented schemas are the same schema.
	head := catalogue
	head.Tools = append([]Tool(nil), catalogue.Tools...)
	head.Tools[0].InputSchema = json.RawMessage("{ \"type\": \"obj\\u0065ct\" }")
	selected, reason := SelectChanged(catalogue, head, cases, cases)
	require.Empty(t, selected)
	require.Contains(t, reason, "no Platform MCP tool or eval case changed")
}

func TestSelectChangedPicksCasesNamingAChangedTool(t *testing.T) {
	t.Parallel()

	base, cases := selectFixture()
	head := base
	head.Tools = append([]Tool(nil), base.Tools...)
	head.Tools[0].Description = "Read one MCP server's tools and settings."
	selected, reason := SelectChanged(base, head, cases, cases)
	require.Equal(t, []string{"read", "disable", "weather"}, caseIDs(selected), "a changed tool in expect or allow selects the case, and negatives come along")
	require.Contains(t, reason, "get_mcp")
}

func TestSelectChangedPicksNewAndEditedCases(t *testing.T) {
	t.Parallel()

	catalogue, base := selectFixture()
	head := append([]Case(nil), base...)
	head[2].Prompt = "List every skill in Default."
	head = append(head, Case{ID: "new", Prompt: "Read Linear.", Expect: []string{"get_mcp"}, Allow: nil, Args: nil, File: "a.yaml"})
	selected, _ := SelectChanged(catalogue, catalogue, base, head)
	require.Equal(t, []string{"skills", "new", "weather"}, caseIDs(selected))
}

func TestSelectChangedRunsEverythingWhenInstructionsChange(t *testing.T) {
	t.Parallel()

	base, cases := selectFixture()
	head := base
	head.Instructions = map[string]string{"direct": "full, reworded", "progressive": "small"}
	selected, reason := SelectChanged(base, head, cases, cases)
	require.Len(t, selected, len(cases))
	require.Contains(t, reason, "instructions changed")
}

func TestChangedToolsIncludesAddedAndRemoved(t *testing.T) {
	t.Parallel()

	base, _ := selectFixture()
	head := base
	head.Tools = []Tool{base.Tools[0], {Name: "enable_mcp", Description: "Turn a server on.", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: false}}
	require.Equal(t, []string{"disable_mcp", "enable_mcp", "list_skills"}, ChangedTools(base, head))
}

func TestSampleCasesIsStable(t *testing.T) {
	t.Parallel()

	_, cases := selectFixture()
	first := SampleCases(cases, 2)
	require.Len(t, first, 2)
	require.Equal(t, caseIDs(first), caseIDs(SampleCases(cases, 2)))
	require.Len(t, SampleCases(cases, 0), len(cases), "zero means no cap")
}

func TestProviderSchemaProblem(t *testing.T) {
	t.Parallel()

	require.Empty(t, ProviderSchemaProblem(json.RawMessage(`{"type":"object","properties":{"a":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}`)), "nested combinators are fine")
	require.Equal(t, "top-level oneOf", ProviderSchemaProblem(json.RawMessage(`{"type":"object","oneOf":[{}]}`)))
	require.Equal(t, "top-level anyOf", ProviderSchemaProblem(json.RawMessage(`{"type":"object","properties":{},"anyOf":[{}]}`)))
	require.Equal(t, `top-level type is not "object"`, ProviderSchemaProblem(json.RawMessage(`{"oneOf":[{}]}`)))
	require.Equal(t, "input schema is not a JSON object", ProviderSchemaProblem(json.RawMessage(`[]`)))
}
