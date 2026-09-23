package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureCandidates() []Candidate {
	return []Candidate{
		{ID: "mcp:slack", Kind: "mcp_server", Title: "Slack", Detail: "MCP server · disabled", Verbs: []string{"open", "enable"}},
		{ID: "page:/settings", Kind: "page", Title: "Settings", Detail: "Page", Verbs: []string{"open"}},
		{ID: "marketplace", Kind: "marketplace", Title: "Plugin marketplace", Detail: "Plugin marketplace · unpublished changes", Verbs: []string{"open", "publish"}},
	}
}

func TestBuildRequestGolden(t *testing.T) {
	t.Parallel()

	req, ids, err := BuildRequest("the disabled slack one", "/mcp", fixtureCandidates())
	require.NoError(t, err)
	require.Equal(t, []string{"mcp:slack", "page:/settings", "marketplace"}, ids)

	got, err := json.Marshal(req)
	require.NoError(t, err)

	want, err := os.ReadFile(filepath.Join("testdata", "build_request.golden.json"))
	require.NoError(t, err)

	// Compare as decoded documents so map key order and whitespace do not
	// matter, while the embedded state keeps its field order for the judge.
	var gotDoc, wantDoc map[string]any
	require.NoError(t, json.Unmarshal(got, &gotDoc))
	require.NoError(t, json.Unmarshal(want, &wantDoc))
	require.Equal(t, wantDoc, gotDoc)
}

func TestBuildRequestDropsPeopleAndRenumbers(t *testing.T) {
	t.Parallel()

	cands := []Candidate{
		{ID: "person:1", Kind: "person", Title: "Ada Lovelace", Detail: "Member · admin", Verbs: []string{"open"}},
		{ID: "page:/settings", Kind: "page", Title: "Settings", Detail: "Page", Verbs: []string{"open"}},
		{ID: "person:2", Kind: "person", Title: "Grace Hopper", Detail: "Member · member", Verbs: []string{"open"}},
		{ID: "plugin:x", Kind: "plugin", Title: "Sales", Detail: "Plugin · 4 servers", Verbs: []string{"open"}},
	}

	req, ids, err := BuildRequest("sett", "", cands)
	require.NoError(t, err)
	require.Equal(t, []string{"page:/settings", "plugin:x"}, ids)

	target := req.Questions[questionTarget].Criteria
	require.Len(t, target, 3)
	require.Contains(t, target, "c0")
	require.Contains(t, target, "c1")
	require.Contains(t, target, "none")
	require.NotContains(t, string(req.State), "Lovelace")
	require.NotContains(t, string(req.State), "Hopper")
	for _, criterion := range target {
		require.NotContains(t, criterion, "Lovelace")
		require.NotContains(t, criterion, "Hopper")
	}

	var doc state
	require.NoError(t, json.Unmarshal(req.State, &doc))
	require.Len(t, doc.Candidates, 2)
	require.Equal(t, "c0", doc.Candidates[0].ID)
	require.Equal(t, "Settings", doc.Candidates[0].Title)
	require.Equal(t, "c1", doc.Candidates[1].ID)
	require.Equal(t, "Sales", doc.Candidates[1].Title)
}

func TestCandidateCriterionLabels(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"Detection rule: Exfil — Rule · enabled (verbs: open)",
		candidateCriterion(Candidate{Kind: "rule", Title: "Exfil", Detail: "Rule · enabled", Verbs: []string{"open"}}),
	)
	require.Equal(t,
		"widget: Thing (verbs: )",
		candidateCriterion(Candidate{Kind: "widget", Title: "Thing"}),
		"unknown kinds use the raw kind and an empty detail is omitted",
	)
}
