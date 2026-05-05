package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockToolsListResponseInterceptor is a [proxy.ToolsListResponseInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockToolsListResponseInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the response observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded result/error
	// through.
	lastCall **proxy.ToolsListResponse
}

func (m *mockToolsListResponseInterceptor) Name() string { return m.name }

func (m *mockToolsListResponseInterceptor) InterceptToolsListResponse(_ context.Context, list *proxy.ToolsListResponse) error {
	if m.called != nil {
		*m.called++
	}
	if m.order != nil {
		*m.order = append(*m.order, m.name)
	}
	if m.lastCall != nil {
		*m.lastCall = list
	}
	return m.err
}
