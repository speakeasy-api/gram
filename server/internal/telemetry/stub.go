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

func (n *StubToolMetricsClient) ListLogsForTrace(_ context.Context, _ repo.ListLogsForTraceParams) ([]repo.TelemetryLog, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) InsertTelemetryLog(_ context.Context, _ repo.InsertTelemetryLogParams) error {
	return nil
}

func (n *StubToolMetricsClient) ShouldLog(_ context.Context, _ string) (bool, error) {
	return true, nil
}
