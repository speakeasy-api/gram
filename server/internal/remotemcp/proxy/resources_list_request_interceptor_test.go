package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockResourcesListRequestInterceptor is a [proxy.ResourcesListRequestInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockResourcesListRequestInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the request observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded params through.
	lastCall **proxy.ResourcesListRequest
}

func (m *mockResourcesListRequestInterceptor) Name() string { return m.name }

func (m *mockResourcesListRequestInterceptor) InterceptResourcesListRequest(_ context.Context, list *proxy.ResourcesListRequest) error {
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
