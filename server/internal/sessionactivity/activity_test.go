package sessionactivity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsMCPMethod(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"tools/call", "resources/read", "prompts/get"} {
		require.True(t, IsMCPMethod(method), method)
		require.True(t, FromContext(WithMCPMethod(context.Background(), method)), method)
	}

	for _, method := range []string{"", "ping", "initialize", "tools/list", "resources/list", "prompts/list"} {
		require.False(t, IsMCPMethod(method), method)
		require.False(t, FromContext(WithMCPMethod(context.Background(), method)), method)
	}

	require.False(t, FromContext(context.Background()))
}
