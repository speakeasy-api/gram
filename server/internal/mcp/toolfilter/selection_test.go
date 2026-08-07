package toolfilter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

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

func TestParseSessionSelection_NullMeansAll(t *testing.T) {
	t.Parallel()

	sel, dropped, err := ParseSessionSelection(nil)
	require.NoError(t, err)
	require.Nil(t, sel)
	require.Empty(t, dropped)
}

func TestParseSessionSelection_MissingResourceFailsClosed(t *testing.T) {
	t.Parallel()

	_, _, err := ParseSessionSelection([]byte(`{"annotations":["read_only"]}`))
	require.Error(t, err)
}

func TestParseSessionSelection_MalformedFailsClosed(t *testing.T) {
	t.Parallel()

	_, _, err := ParseSessionSelection([]byte(`{`))
	require.Error(t, err)
}

func TestParseSessionSelection_UnknownStoredAnnotationsDropped(t *testing.T) {
	t.Parallel()

	sel, dropped, err := ParseSessionSelection([]byte(`{"annotations":["read_only","sneaky_future_value"],"resource":"toolset:x"}`))
	require.NoError(t, err)
	require.Equal(t, []string{AnnotationReadOnly}, sel.Annotations)
	require.Equal(t, []string{"sneaky_future_value"}, dropped)
}

func TestMatchesAnnotations_RawHintsNotDisposition(t *testing.T) {
	t.Parallel()

	// readOnlyHint outranks idempotentHint in the RBAC disposition collapse;
	// the selection matcher must still see the idempotent hint.
	annotations := &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    new(true),
		DestructiveHint: nil,
		IdempotentHint:  new(true),
		OpenWorldHint:   nil,
	}
	require.Equal(t, "read_only", conv.DispositionFromAnnotations(annotations))

	sel := &SessionSelection{Annotations: []string{AnnotationIdempotent}, Tools: nil, Resource: "toolset:x"}
	require.True(t, sel.MatchesAnnotations(annotations))
}

func TestMatchesAnnotations_NilAndFalseFailClosed(t *testing.T) {
	t.Parallel()

	sel := &SessionSelection{Annotations: []string{AnnotationReadOnly}, Tools: nil, Resource: "toolset:x"}
	require.False(t, sel.MatchesAnnotations(nil))
	require.False(t, sel.MatchesAnnotations(&types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    new(false),
		DestructiveHint: nil,
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
	}))
}

func TestFilterToolsBySelection_NilSelectionPassthrough(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{httpToolWithAnnotations("a", nil), proxyTool("p")}
	require.Equal(t, tools, FilterToolsBySelection(tools, nil))
}

func TestFilterToolsBySelection_UnionOfAnnotationsAndNames(t *testing.T) {
	t.Parallel()

	readOnly := httpToolWithAnnotations("reader", &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    new(true),
		DestructiveHint: nil,
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
	})
	writer := httpToolWithAnnotations("writer", nil)
	other := httpToolWithAnnotations("other", nil)

	sel := &SessionSelection{
		Annotations: []string{AnnotationReadOnly},
		Tools:       []string{"writer"},
		Resource:    "toolset:x",
	}
	filtered := FilterToolsBySelection([]*types.Tool{readOnly, writer, other}, sel)
	require.Len(t, filtered, 2)
	require.Equal(t, "reader", filtered[0].HTTPToolDefinition.Name)
	require.Equal(t, "writer", filtered[1].HTTPToolDefinition.Name)
}

func TestFilterToolsBySelection_EmptyAxesMeanZeroTools(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{httpToolWithAnnotations("a", nil)}
	sel := &SessionSelection{Annotations: nil, Tools: nil, Resource: "toolset:x"}
	require.Empty(t, FilterToolsBySelection(tools, sel))
}

func TestFilterToolsBySelection_ProxyToolsFailClosed(t *testing.T) {
	t.Parallel()

	// A proxy placeholder is dropped from every restrictive selection even
	// when its name is explicitly listed: its callable names exist only
	// after upstream unfolding and its hints cannot be verified call-side.
	sel := &SessionSelection{
		Annotations: []string{AnnotationReadOnly},
		Tools:       []string{"p"},
		Resource:    "toolset:x",
	}
	require.Empty(t, FilterToolsBySelection([]*types.Tool{proxyTool("p")}, sel))
}
