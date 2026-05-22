package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockResourcesReadRequestInterceptor is a [proxy.ResourcesReadRequestInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockResourcesReadRequestInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the request observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded params through.
	lastCall **proxy.ResourcesReadRequest
}

func (m *mockResourcesReadRequestInterceptor) Name() string { return m.name }

func (m *mockResourcesReadRequestInterceptor) InterceptResourcesReadRequest(_ context.Context, read *proxy.ResourcesReadRequest) error {
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
