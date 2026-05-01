package xmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewMetrics(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	logger := testenv.NewLogger(t)

	m := newMetrics(meter, logger)
	require.NotNil(t, m)
	require.NotNil(t, m.mcpToolCallCounter)
}

func TestMetrics_RecordMCPToolCall_RecordsWithValidCounter(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	logger := testenv.NewLogger(t)
	m := newMetrics(meter, logger)

	// Should not panic.
	m.RecordMCPToolCall(t.Context(), "org-123", "https://x.example.com/x/mcp/server", "search_tickets")
}

func TestMetrics_RecordMCPToolCall_NilCounterIsSafe(t *testing.T) {
	t.Parallel()

	m := &metrics{mcpToolCallCounter: nil}
	// Should not panic when counter is nil.
	m.RecordMCPToolCall(t.Context(), "org-123", "https://x.example.com/x/mcp/server", "search_tickets")
}
