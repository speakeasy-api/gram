package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// principalToolCall shares caller extraction and refusal handling while leaving
// each tool family responsible for its own safe error projection.
func principalToolCall[Out any](ctx context.Context, refusalFor func(error) (*mcp.CallToolResult, bool), call func(Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err != nil {
		if refusal, ok := refusalFor(err); ok {
			return refusal, zero, nil
		}
		return nil, zero, err
	}
	return nil, output, nil
}
