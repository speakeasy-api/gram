package remotemcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewProxyMetrics(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	logger := testenv.NewLogger(t)

	m := NewProxyMetrics(meter, logger)
	require.NotNil(t, m)
	require.NotNil(t, m.mcpToolCallCounter)
}

func TestProxyMetrics_RecordMCPToolCall_RecordsWithValidCounter(t *testing.T) {
	t.Parallel()

	meter := testenv.NewMeterProvider(t).Meter("test")
	logger := testenv.NewLogger(t)
	m := NewProxyMetrics(meter, logger)

	// Should not panic.
	m.RecordMCPToolCall(t.Context(), "org-123", "https://x.example.com/x/mcp/server", "srv-abc", "search_tickets")
}

func TestProxyMetrics_RecordMCPToolCall_NilCounterIsSafe(t *testing.T) {
	t.Parallel()

	m := &ProxyMetrics{mcpToolCallCounter: nil}
	// Should not panic when counter is nil.
	m.RecordMCPToolCall(t.Context(), "org-123", "https://x.example.com/x/mcp/server", "srv-abc", "search_tickets")
}
