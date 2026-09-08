package toolfilter

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

const (
	testResource = "toolset:0198a0b0-0000-7000-8000-000000000001"
	testGrantID  = "0198a0b0-0000-7000-8000-0000000000aa"
)

func docJSON(allow string) []byte {
	return fmt.Appendf(nil, `{"resource":%q,"grant_id":%q,"allow":%s}`, testResource, testGrantID, allow)
}

func httpTool(name string) *types.Tool {
	return &types.Tool{
		HTTPToolDefinition: &types.HTTPToolDefinition{Name: name},
	}
}

func httpToolWithAnnotations(name string, annotations *types.ToolAnnotations) *types.Tool {
	return &types.Tool{
		HTTPToolDefinition: &types.HTTPToolDefinition{Name: name, Annotations: annotations},
	}
}

func proxyTool(name string) *types.Tool {
	proxy := "proxy"
	return &types.Tool{
		ExternalMcpToolDefinition: &types.ExternalMCPToolDefinition{Name: name, Type: &proxy},
	}
}

func readOnlyAnnotations() *types.ToolAnnotations {
	yes := true
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &yes,
		DestructiveHint: nil,
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
	}
}

func TestParseSessionSelection_NullMeansAll(t *testing.T) {
	t.Parallel()

	sel, err := ParseSessionSelection(nil)
	require.NoError(t, err)
	require.Nil(t, sel)
}

func TestParseSessionSelection_ValidDocumentCompiles(t *testing.T) {
	t.Parallel()

	sel, err := ParseSessionSelection(docJSON(`[
		{"type":"annotation","name":"read_only","mode":"live"},
		{"type":"annotation","name":"destructive","mode":"snapshot","tools":["delete_a","delete_b"]},
		{"type":"tool","name":"special"}
	]`))
	require.NoError(t, err)
	require.True(t, sel.AllowsName("special"))
	require.True(t, sel.AllowsName("delete_a"))
	require.False(t, sel.AllowsName("reader"))
	require.Equal(t, []string{"read_only"}, sel.LiveAnnotations())
}

func TestParseSessionSelection_StrictRejections(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"missing allow":                fmt.Appendf(nil, `{"resource":%q,"grant_id":%q}`, testResource, testGrantID),
		"null allow":                   docJSON(`null`),
		"unknown top-level field":      fmt.Appendf(nil, `{"resource":%q,"grant_id":%q,"allow":[],"extra":1}`, testResource, testGrantID),
		"unknown entry field":          docJSON(`[{"type":"tool","name":"a","bogus":1}]`),
		"unknown entry type":           docJSON(`[{"type":"group","name":"a"}]`),
		"unknown annotation name":      docJSON(`[{"type":"annotation","name":"sneaky","mode":"live"}]`),
		"unknown annotation mode":      docJSON(`[{"type":"annotation","name":"read_only","mode":"eventually"}]`),
		"annotation without mode":      docJSON(`[{"type":"annotation","name":"read_only"}]`),
		"snapshot without tools":       docJSON(`[{"type":"annotation","name":"read_only","mode":"snapshot"}]`),
		"live with tools":              docJSON(`[{"type":"annotation","name":"read_only","mode":"live","tools":[]}]`),
		"tool with mode":               docJSON(`[{"type":"tool","name":"a","mode":"live"}]`),
		"tool with tools":              docJSON(`[{"type":"tool","name":"a","tools":[]}]`),
		"duplicate tool entries":       docJSON(`[{"type":"tool","name":"a"},{"type":"tool","name":"a"}]`),
		"duplicate annotation entries": docJSON(`[{"type":"annotation","name":"read_only","mode":"live"},{"type":"annotation","name":"read_only","mode":"snapshot","tools":[]}]`),
		"duplicate inside snapshot":    docJSON(`[{"type":"annotation","name":"read_only","mode":"snapshot","tools":["a","a"]}]`),
		"blank tool name":              docJSON(`[{"type":"tool","name":""}]`),
		"missing resource":             fmt.Appendf(nil, `{"grant_id":%q,"allow":[]}`, testGrantID),
		"malformed resource":           fmt.Appendf(nil, `{"resource":"toolset:not-a-uuid","grant_id":%q,"allow":[]}`, testGrantID),
		"zero grant id":                fmt.Appendf(nil, `{"resource":%q,"grant_id":"00000000-0000-0000-0000-000000000000","allow":[]}`, testResource),
		"trailing data":                append(docJSON(`[]`), []byte(`{"resource":"x"}`)...),
		"legacy flat tools field":      fmt.Appendf(nil, `{"resource":%q,"grant_id":%q,"allow":[],"tools":["a"]}`, testResource, testGrantID),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseSessionSelection(raw)
			require.Error(t, err)
		})
	}
}

func TestParseSessionSelection_EmptyAllowIsZeroTools(t *testing.T) {
	t.Parallel()

	sel, err := ParseSessionSelection(docJSON(`[]`))
	require.NoError(t, err)
	require.NotNil(t, sel)
	require.False(t, sel.AllowsName("anything"))
	require.Empty(t, sel.LiveAnnotations())
}

func TestNewSessionSelection_CanonicalizesAndValidates(t *testing.T) {
	t.Parallel()

	snapshot := AnnotationModeSnapshot
	live := AnnotationModeLive
	tools := []string{"z", "a"}
	sel, err := NewSessionSelection(testResource, uuid.MustParse(testGrantID), []AllowEntry{
		{Type: AllowTypeTool, Name: "zeta", Mode: nil, Tools: nil},
		{Type: AllowTypeAnnotation, Name: "idempotent", Mode: &live, Tools: nil},
		{Type: AllowTypeTool, Name: "alpha", Mode: nil, Tools: nil},
		{Type: AllowTypeAnnotation, Name: "read_only", Mode: &snapshot, Tools: &tools},
	})
	require.NoError(t, err)

	require.Equal(t, AllowTypeAnnotation, sel.Allow[0].Type)
	require.Equal(t, "read_only", sel.Allow[0].Name)
	require.Equal(t, []string{"a", "z"}, *sel.Allow[0].Tools)
	require.Equal(t, "idempotent", sel.Allow[1].Name)
	require.Equal(t, "alpha", sel.Allow[2].Name)
	require.Equal(t, "zeta", sel.Allow[3].Name)
	// Caller's slice is not mutated by canonical sorting.
	require.Equal(t, []string{"z", "a"}, tools)

	_, err = NewSessionSelection(testResource, uuid.Nil, nil)
	require.Error(t, err)
}

func TestFilterToolsBySelection_NilSelectionPassthrough(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{httpTool("a"), proxyTool("p")}
	require.Equal(t, tools, FilterToolsBySelection(tools, nil))
}

func TestFilterToolsBySelection_SnapshotNamesAndLiveHints(t *testing.T) {
	t.Parallel()

	sel, err := ParseSessionSelection(docJSON(`[
		{"type":"annotation","name":"read_only","mode":"live"},
		{"type":"tool","name":"writer"}
	]`))
	require.NoError(t, err)

	reader := httpToolWithAnnotations("reader", readOnlyAnnotations())
	writer := httpTool("writer")
	other := httpTool("other")

	filtered := FilterToolsBySelection([]*types.Tool{reader, writer, other}, sel)
	require.Len(t, filtered, 2)
	require.Equal(t, "reader", filtered[0].HTTPToolDefinition.Name)
	require.Equal(t, "writer", filtered[1].HTTPToolDefinition.Name)
}

func TestFilterToolsBySelection_SnapshotAnnotationDoesNotTrackHints(t *testing.T) {
	t.Parallel()

	// A snapshot-mode annotation grant is its frozen expansion: a NEW tool
	// carrying the hint stays out until re-consent.
	sel, err := ParseSessionSelection(docJSON(`[
		{"type":"annotation","name":"read_only","mode":"snapshot","tools":["reader"]}
	]`))
	require.NoError(t, err)

	reader := httpToolWithAnnotations("reader", readOnlyAnnotations())
	newcomer := httpToolWithAnnotations("newcomer", readOnlyAnnotations())

	filtered := FilterToolsBySelection([]*types.Tool{reader, newcomer}, sel)
	require.Len(t, filtered, 1)
	require.Equal(t, "reader", filtered[0].HTTPToolDefinition.Name)
}

func TestFilterToolsBySelection_LiveAnnotationTracksHints(t *testing.T) {
	t.Parallel()

	// A live grant picks up a newly-hinted (or renamed) tool immediately.
	sel, err := ParseSessionSelection(docJSON(`[
		{"type":"annotation","name":"read_only","mode":"live"}
	]`))
	require.NoError(t, err)

	renamed := httpToolWithAnnotations("renamed_reader", readOnlyAnnotations())
	unhinted := httpTool("mystery")

	filtered := FilterToolsBySelection([]*types.Tool{renamed, unhinted}, sel)
	require.Len(t, filtered, 1)
	require.Equal(t, "renamed_reader", filtered[0].HTTPToolDefinition.Name)
}

func TestFilterToolsBySelection_ProxyToolsFailClosed(t *testing.T) {
	t.Parallel()

	sel, err := ParseSessionSelection(docJSON(`[{"type":"tool","name":"p"}]`))
	require.NoError(t, err)
	require.Empty(t, FilterToolsBySelection([]*types.Tool{proxyTool("p")}, sel))
}
