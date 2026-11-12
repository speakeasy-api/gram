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
	selected, _, reason := SelectChanged(catalogue, head, cases, cases)
	require.Empty(t, selected)
	require.Contains(t, reason, "no Platform MCP tool or eval case changed")
}

func TestSelectChangedPicksCasesNamingAChangedTool(t *testing.T) {
	t.Parallel()

	base, cases := selectFixture()
	head := base
	head.Tools = append([]Tool(nil), base.Tools...)
	head.Tools[0].Description = "Read one MCP server's tools and settings."
	selected, negatives, reason := SelectChanged(base, head, cases, cases)
	require.Equal(t, []string{"read", "disable"}, caseIDs(selected), "a changed tool in expect or allow selects the case")
	require.Equal(t, []string{"weather"}, caseIDs(negatives), "negatives come along")
	require.Contains(t, reason, "get_mcp")
}

func TestSelectChangedPicksNewAndEditedCases(t *testing.T) {
	t.Parallel()

	catalogue, base := selectFixture()
	head := append([]Case(nil), base...)
	head[2].Prompt = "List every skill in Default."
	head = append(head, Case{ID: "new", Prompt: "Read Linear.", Expect: []string{"get_mcp"}, Allow: nil, Args: nil, File: "a.yaml"})
	selected, negatives, _ := SelectChanged(catalogue, catalogue, base, head)
	require.Equal(t, []string{"skills", "new"}, caseIDs(selected))
	require.Equal(t, []string{"weather"}, caseIDs(negatives))
}

func TestSelectChangedRunsEverythingWhenInstructionsChange(t *testing.T) {
	t.Parallel()

	base, cases := selectFixture()
	head := base
	head.Instructions = map[string]string{"direct": "full, reworded", "progressive": "small"}
	selected, _, reason := SelectChanged(base, head, cases, cases)
	require.Len(t, selected, len(cases))
	require.Contains(t, reason, "instructions changed")
}

func TestSelectChangedRunsEverythingWhenSharedInputsChange(t *testing.T) {
	t.Parallel()

	base, cases := selectFixture()
	prompt := base
	prompt.AgentPrompt = "a reworded environment"
	selected, _, reason := SelectChanged(base, prompt, cases, cases)
	require.Len(t, selected, len(cases))
	require.Contains(t, reason, "agent prompt")

	firstRun := base
	firstRun.FirstRun = []string{"get_mcp"}
	selected, _, reason = SelectChanged(base, firstRun, cases, cases)
	require.Len(t, selected, len(cases))
	require.Contains(t, reason, "discovery surface")
}

func TestChangedToolsSeesReadOnlyAndAnnotationChanges(t *testing.T) {
	t.Parallel()

	base, _ := selectFixture()
	head := base
	head.Tools = append([]Tool(nil), base.Tools...)
	head.Tools[0].ReadOnly = false
	head.Tools[1].Annotations = json.RawMessage(`{"destructiveHint":true}`)
	require.Equal(t, []string{"disable_mcp", "get_mcp"}, ChangedTools(base, head))
}

func TestCapCasesKeepsSelectedCasesFirst(t *testing.T) {
	t.Parallel()

	_, cases := selectFixture()
	selected, filler := cases[:2], cases[2:]
	require.Equal(t, []string{"read", "disable", caseIDs(SampleCases(filler, 1))[0]}, caseIDs(CapCases(selected, filler, 3)))
	require.Len(t, CapCases(selected, filler, 1), 1)
	require.Subset(t, caseIDs(selected), caseIDs(CapCases(selected, filler, 1)))
	require.Len(t, CapCases(selected, filler, 0), len(cases))
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
	require.Equal(t, "top-level allOf", ProviderSchemaProblem(json.RawMessage(`{"type":"object","allOf":[{}]}`)))
	require.Equal(t, `top-level type is not "object"`, ProviderSchemaProblem(json.RawMessage(`{"oneOf":[{}]}`)))
	require.Equal(t, "input schema is not a JSON object", ProviderSchemaProblem(json.RawMessage(`[]`)))
}
