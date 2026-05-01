package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewMetrics(t *testing.T) {
	t.Parallel()

	t.Run("creates_metrics_with_valid_meter", func(t *testing.T) {
		t.Parallel()
		meter := testenv.NewMeterProvider(t).Meter("test")
		logger := testenv.NewLogger(t)

		m := newMetrics(meter, logger)
		require.NotNil(t, m)
		require.NotNil(t, m.mcpToolCallCounter)
		require.NotNil(t, m.mcpRequestDuration)
	})
}

func TestMetrics_RecordMCPToolCall(t *testing.T) {
	t.Parallel()

	t.Run("records_tool_call_with_valid_counter", func(t *testing.T) {
		t.Parallel()
		meter := testenv.NewMeterProvider(t).Meter("test")
		logger := testenv.NewLogger(t)
		m := newMetrics(meter, logger)

		// Should not panic
		m.RecordMCPToolCall(context.Background(), "org-123", "https://mcp.example.com", "test-tool")
	})

	t.Run("handles_nil_counter_gracefully", func(t *testing.T) {
		t.Parallel()
		m := &metrics{
			mcpToolCallCounter: nil,
		}

		// Should not panic when counter is nil
		m.RecordMCPToolCall(context.Background(), "org-123", "https://mcp.example.com", "test-tool")
	})
}

func TestMetrics_RecordMCPRequestDuration(t *testing.T) {
	t.Parallel()

	t.Run("records_duration_with_valid_histogram", func(t *testing.T) {
		t.Parallel()
		meter := testenv.NewMeterProvider(t).Meter("test")
		logger := testenv.NewLogger(t)
		m := newMetrics(meter, logger)

		// Should not panic
		m.RecordMCPRequestDuration(context.Background(), "tools/call", "https://mcp.example.com", 100*time.Millisecond)
	})

	t.Run("handles_nil_histogram_gracefully", func(t *testing.T) {
		t.Parallel()
		m := &metrics{
			mcpRequestDuration: nil,
		}

		// Should not panic when histogram is nil
		m.RecordMCPRequestDuration(context.Background(), "tools/call", "https://mcp.example.com", 100*time.Millisecond)
	})
}
