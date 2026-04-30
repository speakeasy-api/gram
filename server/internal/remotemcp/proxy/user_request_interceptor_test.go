package proxy_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockUserRequestInterceptor is a [proxy.UserRequestInterceptor] used in
// tests to capture invocation order and optionally inject an error or body
// mutation.
type mockUserRequestInterceptor struct {
	name    string
	called  *int
	order   *[]string
	err     error
	mutator func(req *proxy.UserRequest)
}

func (m *mockUserRequestInterceptor) Name() string { return m.name }

func (m *mockUserRequestInterceptor) InterceptUserRequest(_ context.Context, req *proxy.UserRequest) error {
	if m.called != nil {
		*m.called++
	}
	if m.order != nil {
		*m.order = append(*m.order, m.name)
	}
	if m.mutator != nil {
		m.mutator(req)
	}
	return m.err
}
