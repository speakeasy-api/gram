package proxy_test

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockRemoteMessageInterceptor is a [proxy.RemoteMessageInterceptor] used in
// tests to capture invocation order and optionally inject an error. Records
// every message it observes so tests can assert the full sequence of
// messages that passed through the interceptor chain.
type mockRemoteMessageInterceptor struct {
	name     string
	called   *int
	order    *[]string
	err      error
	observed *[]jsonrpc.Message
}

func (m *mockRemoteMessageInterceptor) Name() string { return m.name }

func (m *mockRemoteMessageInterceptor) InterceptRemoteMessage(_ context.Context, msg *proxy.RemoteMessage) error {
	if m.called != nil {
		*m.called++
	}
	if m.order != nil {
		*m.order = append(*m.order, m.name)
	}
	if m.observed != nil {
		*m.observed = append(*m.observed, msg.Message)
	}
	return m.err
}
