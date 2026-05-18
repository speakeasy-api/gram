package proxy_test

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

// mutatingToolsListResponseInterceptor is a
// [proxy.ToolsListResponseInterceptor] that calls
// [proxy.ToolsListResponse.SetTools] with the result of
// toolsFn(currentTools). When err is non-nil, the interceptor returns it
// AFTER mutating — used by tests covering the "mutate-then-reject"
// composition.
type mutatingToolsListResponseInterceptor struct {
	name    string
	toolsFn func([]*mcp.Tool) []*mcp.Tool
	err     error
}

func (m *mutatingToolsListResponseInterceptor) Name() string { return m.name }

func (m *mutatingToolsListResponseInterceptor) InterceptToolsListResponse(_ context.Context, list *proxy.ToolsListResponse) error {
	if m.toolsFn != nil {
		var current []*mcp.Tool
		if list.Result != nil {
			current = list.Result.Tools
		}
		newTools := m.toolsFn(current)
		if err := list.SetTools(newTools); err != nil {
			return fmt.Errorf("mutating interceptor %s: %w", m.name, err)
		}
	}
	return m.err
}

// observingToolsListResponseInterceptor records the number of tools it
// observed on the response so tests can assert later interceptors see
// earlier interceptors' mutations.
type observingToolsListResponseInterceptor struct {
	name        string
	observedLen *int32
}

func (o *observingToolsListResponseInterceptor) Name() string { return o.name }

func (o *observingToolsListResponseInterceptor) InterceptToolsListResponse(_ context.Context, list *proxy.ToolsListResponse) error {
	if o.observedLen != nil {
		atomic.StoreInt32(o.observedLen, int32(len(list.Result.Tools)))
	}
	return nil
}
