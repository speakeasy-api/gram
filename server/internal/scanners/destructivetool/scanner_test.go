package destructivetool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// fakeResolver is a test double for Resolver keyed by the bare (post-variation)
// tool name the scanner passes in.
type fakeResolver struct {
	byName map[string]*shadowmcp.ResolvedToolCall
	calls  []resolveCall
}

type resolveCall struct {
	toolName string
	orgID    string
}

func (f *fakeResolver) ResolveToolsetCall(_ context.Context, _ any, toolName string, orgID string) (*shadowmcp.ResolvedToolCall, bool) {
	f.calls = append(f.calls, resolveCall{toolName: toolName, orgID: orgID})
	resolved, ok := f.byName[toolName]
	return resolved, ok
}

func resolvedTool(name string, destructive *bool) *shadowmcp.ResolvedToolCall {
	var annotations *types.ToolAnnotations
	if destructive != nil {
		annotations = &types.ToolAnnotations{
			Title:           nil,
			ReadOnlyHint:    nil,
			DestructiveHint: destructive,
			IdempotentHint:  nil,
			OpenWorldHint:   nil,
		}
	}
	return &shadowmcp.ResolvedToolCall{
		ToolsetID: "toolset-1",
		ToolName:  name,
		Tool: types.BaseToolAttributes{
			ID:            "tool-1",
			ToolUrn:       "",
			ProjectID:     "",
			Name:          name,
			CanonicalName: name,
			Description:   "",
			SchemaVersion: nil,
			Schema:        "",
			Confirm:       nil,
			ConfirmPrompt: nil,
			Summarizer:    nil,
			CreatedAt:     "",
			UpdatedAt:     "",
			Canonical:     nil,
			Variation:     nil,
			Annotations:   annotations,
		},
	}
}

func TestScanner_FlagsDestructiveAnnotatedTool(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{
		byName: map[string]*shadowmcp.ResolvedToolCall{
			"delete_records": resolvedTool("delete_records", new(true)),
		},
		calls: nil,
	}
	s := NewScanner(resolver)

	findings := s.Scan(t.Context(), "org-1", []ToolCall{
		{Name: "mcp__gram__delete_records", Arguments: `{"x-gram-toolset-id":"abc"}`},
	})

	if assert.Len(t, findings, 1) {
		assert.Equal(t, Source, findings[0].Source)
		assert.Equal(t, Rule, findings[0].RuleID)
		assert.Equal(t, "delete_records", findings[0].Match)
		assert.Contains(t, findings[0].Description, "delete_records")
		assert.Contains(t, findings[0].Description, "destructive")
	}

	// The bare, post-variation function name is what the resolver receives.
	if assert.Len(t, resolver.calls, 1) {
		assert.Equal(t, "delete_records", resolver.calls[0].toolName)
		assert.Equal(t, "org-1", resolver.calls[0].orgID)
	}
}

func TestScanner_SkipsNonDestructiveAndUnhintedTools(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{
		byName: map[string]*shadowmcp.ResolvedToolCall{
			"false_hint": resolvedTool("false_hint", new(false)),
			"no_hint":    resolvedTool("no_hint", nil),
		},
		calls: nil,
	}
	s := NewScanner(resolver)

	findings := s.Scan(t.Context(), "org-1", []ToolCall{
		{Name: "mcp__gram__false_hint", Arguments: `{"x-gram-toolset-id":"abc"}`},
		{Name: "mcp__gram__no_hint", Arguments: `{"x-gram-toolset-id":"abc"}`},
	})
	assert.Empty(t, findings)
}

func TestScanner_SkipsUnresolvedAndNonMCPAndMalformed(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{
		byName: map[string]*shadowmcp.ResolvedToolCall{
			"delete_records": resolvedTool("delete_records", new(true)),
		},
		calls: nil,
	}
	s := NewScanner(resolver)

	findings := s.Scan(t.Context(), "org-1", []ToolCall{
		{Name: "", Arguments: `{}`},                               // nameless
		{Name: "Bash", Arguments: `{}`},                           // native, non-MCP
		{Name: "mcp__gram__unknown", Arguments: `{}`},             // resolver returns !ok
		{Name: "mcp__gram__delete_records", Arguments: `{"bad":`}, // malformed JSON
	})
	assert.Empty(t, findings)

	// Only the MCP call with parseable arguments reaches the resolver.
	if assert.Len(t, resolver.calls, 1) {
		assert.Equal(t, "unknown", resolver.calls[0].toolName)
	}
}

func TestScanner_EmptyArgumentsResolve(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{
		byName: map[string]*shadowmcp.ResolvedToolCall{
			"delete_records": resolvedTool("delete_records", new(true)),
		},
		calls: nil,
	}
	s := NewScanner(resolver)

	// Empty arguments are valid input (nil tool input) and must still resolve.
	findings := s.Scan(t.Context(), "org-1", []ToolCall{
		{Name: "mcp__gram__delete_records", Arguments: ""},
	})
	assert.Len(t, findings, 1)
}

func TestDescribe_ReturnsCanonicalRuleID(t *testing.T) {
	t.Parallel()

	id, desc := describe("delete_records")
	assert.Equal(t, Rule, id)
	require.NoError(t, scanners.ValidateRuleID(id))
	assert.Contains(t, desc, "delete_records")

	idNoTool, descNoTool := describe("")
	assert.Equal(t, Rule, idNoTool)
	assert.NotEmpty(t, descNoTool)
}
