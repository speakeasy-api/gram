package toolmetrics

import (
	"context"
)

type StubToolMetricsClient struct{}

func (n *StubToolMetricsClient) List(_ context.Context, _ ListToolLogsOptions) (*ListResult, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) Log(_ context.Context, _ ToolHTTPRequest) error {
	return nil
}

func (n *StubToolMetricsClient) ShouldLog(_ context.Context, _ string) (bool, error) {
	return true, nil
}
