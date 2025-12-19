package telemetry

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type StubToolMetricsClient struct{}

func (n *StubToolMetricsClient) ListHTTPRequests(_ context.Context, _ repo.ListToolLogsOptions) (*repo.ListResult, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) ListToolLogs(_ context.Context, _ repo.ListToolLogsParams) (*repo.ToolLogsListResult, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) ListTelemetryLogs(_ context.Context, _ repo.ListTelemetryLogsParams) ([]repo.TelemetryLog, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) LogHTTPRequest(_ context.Context, _ repo.ToolHTTPRequest) error {
	return nil
}

func (n *StubToolMetricsClient) ShouldLog(_ context.Context, _ string) (bool, error) {
	return true, nil
}
