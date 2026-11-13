package tooleval

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestTruncateKeepsUTF8AndLimit(t *testing.T) {
	t.Parallel()

	require.Equal(t, "short", truncate("short", 10))
	text := strings.Repeat("é", 20) // two bytes each
	for limit := 4; limit < 20; limit++ {
		cut := truncate(text, limit)
		require.True(t, utf8.ValidString(cut), "limit %d", limit)
		require.LessOrEqual(t, len(cut), limit, "limit %d includes the ellipsis", limit)
		require.True(t, strings.HasSuffix(cut, "…"))
	}
}

func reportCase(id string, expect []string, args map[string]any) Case {
	return Case{ID: id, Prompt: "p", Expect: expect, Allow: nil, Args: args, File: "a.yaml"}
}

func TestMissesShowsUnexpectedArgumentsAndUnfinishedAgents(t *testing.T) {
	t.Parallel()

	wrongArgs := Outcome{Model: "m", Surface: "s", Case: reportCase("rename", []string{"update_mcp_metadata"}, map[string]any{"name": "Tracker"}),
		Selected: &Call{Tool: "update_mcp_metadata", Arguments: json.RawMessage(`{"name":"Other"}`)}, Hit: true, SchemaValid: true, ArgsMatch: false}
	unfinished := Outcome{Model: "m", Surface: "s", Case: reportCase("weather", nil, nil), Unfinished: true}
	right := Outcome{Model: "m", Surface: "s", Case: reportCase("read", []string{"get_mcp"}, nil),
		Selected: &Call{Tool: "get_mcp", Arguments: json.RawMessage(`{}`)}, Hit: true, SchemaValid: true}

	report := newReport(Config{Models: []string{"m"}, Runs: 1, Concurrency: 1}, []Surface{{Name: "s"}}, []Outcome{wrongArgs, unfinished, right})
	misses := map[string]Miss{}
	for _, m := range report.Misses() {
		misses[m.CaseID] = m
	}
	require.Equal(t, map[string]int{"update_mcp_metadata (unexpected arguments)": 1}, misses["rename"].Chosen)
	require.Equal(t, map[string]int{"(unfinished)": 1}, misses["weather"].Chosen)
	require.NotContains(t, misses, "read")
	require.Contains(t, report.Markdown(), "no tool call or answer within the turn or time limit")
}
