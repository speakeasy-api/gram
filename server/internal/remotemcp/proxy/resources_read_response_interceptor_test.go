package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockResourcesReadResponseInterceptor is a [proxy.ResourcesReadResponseInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockResourcesReadResponseInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the response observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded result/error
	// through.
	lastCall **proxy.ResourcesReadResponse
}

func (m *mockResourcesReadResponseInterceptor) Name() string { return m.name }

func (m *mockResourcesReadResponseInterceptor) InterceptResourcesReadResponse(_ context.Context, read *proxy.ResourcesReadResponse) error {
	if m.called != nil {
		*m.called++
	}
	if m.order != nil {
		*m.order = append(*m.order, m.name)
	}
	if m.lastCall != nil {
		*m.lastCall = read
	}
	return m.err
}
