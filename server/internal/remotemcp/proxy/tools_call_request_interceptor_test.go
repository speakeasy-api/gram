package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockToolsCallRequestInterceptor is a [proxy.ToolsCallRequestInterceptor]
// used in tests to capture invocation order and optionally inject an error.
type mockToolsCallRequestInterceptor struct {
	name   string
	called *int
	order  *[]string
	err    error
	// lastCall captures the request observed on the most recent invocation,
	// so tests can assert the proxy threaded the decoded params through.
	lastCall **proxy.ToolsCallRequest
}

func (m *mockToolsCallRequestInterceptor) Name() string { return m.name }

func (m *mockToolsCallRequestInterceptor) InterceptToolsCallRequest(_ context.Context, call *proxy.ToolsCallRequest) error {
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

// mutatingToolsCallRequestInterceptor is a [proxy.ToolsCallRequestInterceptor]
// that calls [proxy.ToolsCallRequest.SetArguments] with the result of
// argsFn(currentArguments). When err is non-nil, the interceptor returns
// it AFTER mutating — used by tests covering the "mutate-then-reject"
// composition.
type mutatingToolsCallRequestInterceptor struct {
	name   string
	argsFn func(json.RawMessage) json.RawMessage
	err    error
}

func (m *mutatingToolsCallRequestInterceptor) Name() string { return m.name }

func (m *mutatingToolsCallRequestInterceptor) InterceptToolsCallRequest(_ context.Context, call *proxy.ToolsCallRequest) error {
	if m.argsFn != nil {
		if err := call.SetArguments(m.argsFn(call.Params.Arguments)); err != nil {
			return fmt.Errorf("mutating interceptor %s: %w", m.name, err)
		}
	}
	return m.err
}
