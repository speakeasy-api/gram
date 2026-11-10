package otel

import (
	"testing"

	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func connectionLogTestRecord(t *testing.T) *otelv1.LogRecord {
	t.Helper()
	return agentEventTestLog(t, dialect.ConnectionLogScope, "speakeasy.mcp_connection")
}

func TestAgentEventLogCHWriterLeavesOutConnectionLogs(t *testing.T) {
	t.Parallel()

	inserter := &captureAgentEventInserter{batches: nil, err: nil}
	writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)

	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{connectionLogTestRecord(t)}, nil))
	require.Empty(t, inserter.batches)
}

func TestLogEventCHWriterLeavesOutConnectionLogs(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{connectionLogTestRecord(t)}, nil))
	require.Empty(t, inserter.batches)
}
