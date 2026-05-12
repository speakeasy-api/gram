package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockInitializeRequestInterceptor is a [proxy.InitializeRequestInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockInitializeRequestInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the request observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded params through.
	lastCall **proxy.InitializeRequest
}

func (m *mockInitializeRequestInterceptor) Name() string { return m.name }

func (m *mockInitializeRequestInterceptor) InterceptInitializeRequest(_ context.Context, init *proxy.InitializeRequest) error {
	if m.called != nil {
		*m.called++
	}
	if m.order != nil {
		*m.order = append(*m.order, m.name)
	}
	if m.lastCall != nil {
		*m.lastCall = init
	}
	return m.err
}
