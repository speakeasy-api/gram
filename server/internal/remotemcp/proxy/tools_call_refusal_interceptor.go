package proxy

import "context"

// ToolsCallRefusalInterceptor is told when an interceptor refused a "tools/call"
// on its way out or its reply on the way back. It only observes: the error
// the user gets is already decided.
type ToolsCallRefusalInterceptor interface {
	// InterceptToolsCallRefusal is called with the refusing interceptor's own error.
	InterceptToolsCallRefusal(ctx context.Context, err error)
}
