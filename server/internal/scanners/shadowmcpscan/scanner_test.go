package shadowmcpscan

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// discardLogger returns a logger that discards output. This internal test
// package cannot use testenv.NewLogger: testenv transitively imports
// shadowmcpscan, so importing it here would create an import cycle.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler) //nolint:forbidigo // testenv.NewLogger would import-cycle this package's internal tests
}

// fakeValidator denies any MCP call whose bare (post-variation) tool name is in
// denied. It records the org id it was called with.
type fakeValidator struct {
	denied map[string]bool
	orgIDs []string
}

func (f *fakeValidator) ValidateToolsetCall(_ context.Context, _ any, toolName string, orgID string) (string, bool) {
	f.orgIDs = append(f.orgIDs, orgID)
	if f.denied[toolName] {
		return "denied: " + toolName, true
	}
	return "", false
}

// fakeLookup returns matches for the ids it is configured with, or an error.
type fakeLookup struct {
	matches map[string]string
	err     error
	gotIDs  []string
}

func (f *fakeLookup) LookupMCPMatchesByToolCallID(_ context.Context, _ uuid.UUID, ids []string) (map[string]string, error) {
	f.gotIDs = append(f.gotIDs, ids...)
	if f.err != nil {
		return nil, f.err
	}
	return f.matches, nil
}

// emptyLookup is the null-object equivalent used by tests that don't exercise
// enrichment: it returns no matches, so findings keep their fallback Match.
// Callers never pass nil — the scanner's matchLookup is always non-nil.
func emptyLookup() *fakeLookup {
	return &fakeLookup{matches: map[string]string{}, err: nil, gotIDs: nil}
}

func TestScanner_FlagsDeniedMCPCall_ServerPrefixFallback(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	s := NewScanner(discardLogger(), validator, emptyLookup())

	out := s.Scan(t.Context(), "org-1", uuid.New(), [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`}},
	})

	require.Len(t, out, 1)
	require.Len(t, out[0], 1)
	f := out[0][0]
	assert.Equal(t, Source, f.Source)
	assert.Equal(t, Rule, f.RuleID)
	assert.Equal(t, "db", f.Match, "no lookup: falls back to the tool name's server prefix")
	assert.Equal(t, "call-1", f.McpLookupToolCallID)
	assert.Contains(t, f.Description, "mcp__db__delete")
	assert.Equal(t, []string{"org-1"}, validator.orgIDs)
}

func TestScanner_EnrichesMatchFromLookup(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	lookup := &fakeLookup{matches: map[string]string{"call-1": "https://evil.example/mcp"}, err: nil, gotIDs: nil}
	s := NewScanner(discardLogger(), validator, lookup)

	projectID := uuid.New()
	out := s.Scan(t.Context(), "org-1", projectID, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`}},
	})

	require.Len(t, out[0], 1)
	assert.Equal(t, "https://evil.example/mcp", out[0][0].Match, "lookup hit overrides the fallback Match")
	assert.Equal(t, []string{"call-1"}, lookup.gotIDs)
}

func TestScanner_LookupErrorKeepsFallback(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	lookup := &fakeLookup{matches: nil, err: errors.New("boom"), gotIDs: nil}
	s := NewScanner(discardLogger(), validator, lookup)

	out := s.Scan(t.Context(), "org-1", uuid.New(), [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`}},
	})
	require.Len(t, out[0], 1)
	assert.Equal(t, "db", out[0][0].Match, "lookup error leaves the server-prefix fallback in place")
}

func TestScanner_LookupMissKeepsFallback(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	// Miss (empty string) must not overwrite the fallback with "".
	lookup := &fakeLookup{matches: map[string]string{"call-1": ""}, err: nil, gotIDs: nil}
	s := NewScanner(discardLogger(), validator, lookup)

	out := s.Scan(t.Context(), "org-1", uuid.New(), [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`}},
	})
	require.Len(t, out[0], 1)
	assert.Equal(t, "db", out[0][0].Match)
}

func TestScanner_SkipsNonMCPNamelessAndAllowed(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	s := NewScanner(discardLogger(), validator, emptyLookup())

	out := s.Scan(t.Context(), "org-1", uuid.New(), [][]ToolCall{
		{
			{ID: "a", Name: "", Arguments: `{}`},              // nameless
			{ID: "b", Name: "Bash", Arguments: `{}`},          // native, non-MCP
			{ID: "c", Name: "mcp__db__read", Arguments: `{}`}, // MCP but not denied
		},
	})
	require.Len(t, out, 1)
	assert.Empty(t, out[0])
	// Only the MCP-routed call reaches the validator.
	assert.Equal(t, []string{"org-1"}, validator.orgIDs)
}

func TestScanner_BatchPositionalAlignment(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	lookup := &fakeLookup{
		matches: map[string]string{"call-0": "match-0", "call-2": "match-2"},
		err:     nil,
		gotIDs:  nil,
	}
	s := NewScanner(discardLogger(), validator, lookup)

	out := s.Scan(t.Context(), "org-1", uuid.New(), [][]ToolCall{
		{{ID: "call-0", Name: "mcp__db__delete", Arguments: `{}`}}, // denied
		{{ID: "call-1", Name: "mcp__db__read", Arguments: `{}`}},   // allowed
		{{ID: "call-2", Name: "mcp__fs__delete", Arguments: `{}`}}, // denied
	})

	require.Len(t, out, 3)
	require.Len(t, out[0], 1)
	assert.Equal(t, "match-0", out[0][0].Match)
	assert.Empty(t, out[1])
	require.Len(t, out[2], 1)
	assert.Equal(t, "match-2", out[2][0].Match)

	// Both denied ids are looked up in a single batched call.
	assert.ElementsMatch(t, []string{"call-0", "call-2"}, lookup.gotIDs)
}

func TestScanner_NoDeniedCallsSkipsLookup(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	lookup := &fakeLookup{matches: map[string]string{}, err: nil, gotIDs: nil}
	s := NewScanner(discardLogger(), validator, lookup)

	out := s.Scan(t.Context(), "org-1", uuid.New(), [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__read", Arguments: `{}`}},
	})
	require.Empty(t, out[0])
	assert.Empty(t, lookup.gotIDs, "no denied calls: the lookup is never issued")
}

func TestDescribe_ReturnsCanonicalRuleID(t *testing.T) {
	t.Parallel()

	id, desc := describe("mcp__db__delete")
	assert.Equal(t, Rule, id)
	require.NoError(t, scanners.ValidateRuleID(id))
	assert.Contains(t, desc, "mcp__db__delete")
	assert.NotContains(t, desc, "x-gram-toolset-id", "must not leak validator internals")

	idNoTool, descNoTool := describe("")
	assert.Equal(t, Rule, idNoTool)
	assert.NotEmpty(t, descNoTool)
}
