package proxy_test

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockResourcesListResponseInterceptor is a [proxy.ResourcesListResponseInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockResourcesListResponseInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the response observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded result/error
	// through.
	lastCall **proxy.ResourcesListResponse
}

func (m *mockResourcesListResponseInterceptor) Name() string { return m.name }

func (m *mockResourcesListResponseInterceptor) InterceptResourcesListResponse(_ context.Context, list *proxy.ResourcesListResponse) error {
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

// mutatingResourcesListResponseInterceptor is a
// [proxy.ResourcesListResponseInterceptor] that calls
// [proxy.ResourcesListResponse.SetResources] with the result of
// resourcesFn(currentResources). When err is non-nil, the interceptor
// returns it AFTER mutating — used by tests covering the
// "mutate-then-reject" composition.
type mutatingResourcesListResponseInterceptor struct {
	name        string
	resourcesFn func([]*mcp.Resource) []*mcp.Resource
	err         error
}

func (m *mutatingResourcesListResponseInterceptor) Name() string { return m.name }

func (m *mutatingResourcesListResponseInterceptor) InterceptResourcesListResponse(_ context.Context, list *proxy.ResourcesListResponse) error {
	if m.resourcesFn != nil {
		var current []*mcp.Resource
		if list.Result != nil {
			current = list.Result.Resources
		}
		newResources := m.resourcesFn(current)
		if err := list.SetResources(newResources); err != nil {
			return fmt.Errorf("mutating interceptor %s: %w", m.name, err)
		}
	}
	return m.err
}
