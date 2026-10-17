package toolsets

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// TestTriggerToolsetIndexAsksWhetherAnIndexIsNeededBeforeWhetherItCanSchedule
// pins the order of the two early returns in TriggerToolsetIndex.
//
// The errors mean opposite things to a caller: ErrToolsetIndexNotRequired is a
// success, while ErrToolsetIndexUnavailable tells an agent (through
// index_signal) that a dynamic-mode server may be unable to list its tools.
// Checking "can I schedule?" first reports unavailable for a version that
// needed no index at all, which is a false alarm the Platform MCP skill tells
// the agent to act on.
//
// The nil temporal environment here is the real condition in a stack with no
// Temporal configured, so this is the combination that regresses.
func TestTriggerToolsetIndexAsksWhetherAnIndexIsNeededBeforeWhetherItCanSchedule(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	// Never dialed: an empty version returns before the pool is used. It only
	// has to be non-nil to clear the argument guard. A version with tools is
	// checked against the database, so its cases need a real one.
	cfg, err := pgxpool.ParseConfig("postgres://unused:unused@127.0.0.1:1/unused")
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	enabled := true
	empty := &types.Toolset{ID: uuid.NewString(), ProjectID: uuid.NewString(), McpEnabled: &enabled, Tools: nil}
	// temporalEnv is nil: that is the whole point.
	require.ErrorIs(t, TriggerToolsetIndex(ctx, logger, pool, nil, empty), ErrToolsetIndexNotRequired,
		"dynamic mode serves an empty version without an index, so nothing was lost by not scheduling")
}
