package telemetry

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type StubToolMetricsClient struct{}

func (n *StubToolMetricsClient) ListTelemetryLogs(_ context.Context, _ repo.ListTelemetryLogsParams) ([]repo.TelemetryLog, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) ListTraces(_ context.Context, _ repo.ListTracesParams) ([]repo.TraceSummary, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) ListChats(_ context.Context, _ repo.ListChatsParams) ([]repo.ChatSummary, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) InsertTelemetryLog(_ context.Context, _ repo.InsertTelemetryLogParams) error {
	return nil
}
