package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockToolsCallResponseInterceptor is a [proxy.ToolsCallResponseInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockToolsCallResponseInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the response observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded result/error
	// through.
	lastCall **proxy.ToolsCallResponse
}

func (m *mockToolsCallResponseInterceptor) Name() string { return m.name }

func (m *mockToolsCallResponseInterceptor) InterceptToolsCallResponse(_ context.Context, call *proxy.ToolsCallResponse) error {
	if m.called != nil {
		*m.called++
	}
	if m.order != nil {
		*m.order = append(*m.order, m.name)
	}
	if m.lastCall != nil {
		*m.lastCall = call
	}
	return m.err
}
