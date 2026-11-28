package sessionactivity

import "context"

type contextKey struct{}

// WithMCPMethod records whether method represents real MCP work that should
// extend ingress and egress session activity windows.
func WithMCPMethod(ctx context.Context, method string) context.Context {
	return context.WithValue(ctx, contextKey{}, IsMCPMethod(method))
}

// IsMCPMethod reports whether an MCP method represents real work rather than
// connection maintenance or catalog discovery.
func IsMCPMethod(method string) bool {
	switch method {
	case "tools/call", "resources/read", "prompts/get":
		return true
	default:
		return false
	}
}

// FromContext reports whether the current request represents real MCP work.
// A missing marker is conservative: non-MCP and maintenance paths do not count.
func FromContext(ctx context.Context) bool {
	active, _ := ctx.Value(contextKey{}).(bool)
	return active
}
