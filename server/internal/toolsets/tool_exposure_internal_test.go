package toolsets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func toolURNs(t *testing.T, values ...string) []urn.Tool {
	t.Helper()
	tools := make([]urn.Tool, 0, len(values))
	for _, value := range values {
		parsed, err := urn.ParseTool(value)
		require.NoError(t, err)
		tools = append(tools, parsed)
	}
	return tools
}

func toolStrings(tools []urn.Tool) []string {
	values := make([]string, 0, len(tools))
	for _, tool := range tools {
		values = append(values, tool.String())
	}
	return values
}

// The whole point of computing the change here rather than taking a
// replacement list is that tools the caller never mentioned survive it.
func TestApplyToolExposureChangeAddsWithoutDisturbingTheRest(t *testing.T) {
	t.Parallel()

	current := toolURNs(t, "tools:function:orders:create_order", "tools:http:billing:get_invoice")
	after, applied, unchanged := applyToolExposureChange(current, ToolExposureChange{
		Add: toolURNs(t, "tools:function:orders:cancel_order", "tools:http:billing:get_invoice"),
	})

	require.Equal(t, []string{
		"tools:function:orders:create_order",
		"tools:http:billing:get_invoice",
		"tools:function:orders:cancel_order",
	}, toolStrings(after))
	require.Equal(t, []string{"tools:function:orders:cancel_order"}, toolStrings(applied))
	require.Equal(t, []string{"tools:http:billing:get_invoice"}, toolStrings(unchanged),
		"a tool already exposed is reported as unchanged, not added twice")
	require.Equal(t, []string{"tools:function:orders:create_order", "tools:http:billing:get_invoice"}, toolStrings(current),
		"the committed list read from the version chain is never mutated in place")
}

func TestApplyToolExposureChangeRemovesOnlyWhatWasNamed(t *testing.T) {
	t.Parallel()

	current := toolURNs(t, "tools:function:orders:create_order", "tools:http:billing:get_invoice", "tools:function:orders:cancel_order")
	after, applied, unchanged := applyToolExposureChange(current, ToolExposureChange{
		Remove: toolURNs(t, "tools:http:billing:get_invoice", "tools:function:orders:refund_order"),
	})

	require.Equal(t, []string{"tools:function:orders:create_order", "tools:function:orders:cancel_order"}, toolStrings(after))
	require.Equal(t, []string{"tools:http:billing:get_invoice"}, toolStrings(applied))
	require.Equal(t, []string{"tools:function:orders:refund_order"}, toolStrings(unchanged),
		"removing a tool the server never exposed is a no-op, not a failure")
}

// A request that changes nothing must be distinguishable from one that does,
// so the caller can report "already there" instead of announcing a republish.
func TestApplyToolExposureChangeReportsANoOp(t *testing.T) {
	t.Parallel()

	current := toolURNs(t, "tools:function:orders:create_order")
	_, applied, unchanged := applyToolExposureChange(current, ToolExposureChange{Add: toolURNs(t, "tools:function:orders:create_order")})
	require.Empty(t, applied)
	require.Len(t, unchanged, 1)

	_, applied, unchanged = applyToolExposureChange(current, ToolExposureChange{Remove: toolURNs(t, "tools:http:billing:get_invoice")})
	require.Empty(t, applied)
	require.Len(t, unchanged, 1)
}

func TestApplyToolExposureChangeStartsFromAnEmptyToolset(t *testing.T) {
	t.Parallel()

	after, applied, unchanged := applyToolExposureChange(nil, ToolExposureChange{Add: toolURNs(t, "tools:function:orders:create_order")})
	require.Equal(t, []string{"tools:function:orders:create_order"}, toolStrings(after))
	require.Len(t, applied, 1)
	require.Empty(t, unchanged)
}
