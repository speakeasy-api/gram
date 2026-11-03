package mcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogDiffSchemaAndDescription(t *testing.T) {
	t.Parallel()
	before := &toolListEntry{Name: "source--search", Description: "Find records", InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","maximum":10}}}`)}
	after := &toolListEntry{Name: before.Name, Description: "Find and rank records", InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","maximum":100}}}`), OutputSchema: json.RawMessage(`{"type":"array"}`)}
	fields, err := compareToolDefinition(before, after)
	require.NoError(t, err)
	require.Len(t, fields, 3)
	require.Equal(t, "description", fields[0].Name)
	require.Equal(t, before.Description, fields[0].Before)
	require.Equal(t, after.Description, fields[0].After)
	require.Equal(t, "inputSchema", fields[1].Name)
	require.JSONEq(t, string(before.InputSchema), fields[1].Before)
	require.JSONEq(t, string(after.InputSchema), fields[1].After)
	require.Equal(t, "outputSchema", fields[2].Name)
	require.False(t, fields[2].HasBefore)
	require.True(t, fields[2].HasAfter)
}

func TestCatalogDiffIgnoresSchemaFormatting(t *testing.T) {
	t.Parallel()
	before := &toolListEntry{Name: "source--search", InputSchema: json.RawMessage(`{"type":"integer", "maximum":9007199254740993}`)}
	after := &toolListEntry{Name: before.Name, InputSchema: json.RawMessage(`{"maximum":9007199254740993,"type":"integer"}`)}
	fields, err := compareToolDefinition(before, after)
	require.NoError(t, err)
	require.Empty(t, fields)
	after.InputSchema = json.RawMessage(`{"maximum":9007199254740992,"type":"integer"}`)
	fields, err = compareToolDefinition(before, after)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Contains(t, fields[0].Before, "9007199254740993")
	require.Contains(t, fields[0].After, "9007199254740992")
}

func TestCatalogDiffAbsentNullAndEmpty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		before *toolListEntry
		after  *toolListEntry
		want   catalogFieldChange
	}{
		{"added empty description", nil, &toolListEntry{Name: "search"}, catalogFieldChange{Name: "description", After: `""`, HasAfter: true}},
		{"removed description", &toolListEntry{Name: "search", Description: "Search"}, nil, catalogFieldChange{Name: "description", Before: "Search", HasBefore: true}},
		{"added null schema", &toolListEntry{Name: "search"}, &toolListEntry{Name: "search", InputSchema: json.RawMessage(`null`)}, catalogFieldChange{Name: "inputSchema", After: "null", HasAfter: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fields, err := compareToolDefinition(tc.before, tc.after)
			require.NoError(t, err)
			require.Equal(t, []catalogFieldChange{tc.want}, fields)
		})
	}
}

func TestCatalogDiffEscapesToolContent(t *testing.T) {
	t.Parallel()
	tool := &toolListEntry{Name: "source--<script>alert(1)</script>", Description: `<img src=x onerror=alert(1)>`, InputSchema: json.RawMessage(`{"description":"<script>alert(2)</script>"}`)}
	fields, err := compareToolDefinition(nil, tool)
	require.NoError(t, err)
	var html bytes.Buffer
	require.NoError(t, consentTemplate.ExecuteTemplate(&html, "catalogToolDiff", catalogToolChange{Name: tool.Name, Fields: fields}))
	require.NotContains(t, html.String(), "<script>")
	require.NotContains(t, html.String(), "<img")
	require.Contains(t, html.String(), "&lt;img")
	require.NotContains(t, html.String(), "− Before")
	require.Contains(t, html.String(), "+ After")
	require.Contains(t, html.String(), "inputSchema")
}

func TestCatalogDiffInvalidDefinition(t *testing.T) {
	t.Parallel()
	_, err := compareToolDefinition(nil, &toolListEntry{InputSchema: json.RawMessage(`{`)})
	require.ErrorContains(t, err, "encode tool definition")
}
