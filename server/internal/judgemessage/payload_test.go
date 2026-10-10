package judgemessage

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestToolIdentityWithinLimitIsUnchanged(t *testing.T) {
	t.Parallel()
	identity := strings.Repeat("界", maxPayloadToolIdentityLen)
	native := payloadTool(identity, "", "")
	require.Equal(t, identity, native.Name)
	require.False(t, native.IdentityTruncated)
	mcp := payloadTool("", identity, identity)
	require.Equal(t, identity, mcp.MCPServer)
	require.Equal(t, identity, mcp.MCPFunction)
	require.False(t, mcp.IdentityTruncated)
	require.Nil(t, payloadTool("", "", ""))
}

func TestToolIdentityTruncationIsExplicit(t *testing.T) {
	t.Parallel()
	identity := strings.Repeat("界", maxPayloadToolIdentityLen+1)
	native := payloadTool(identity, "", "")
	require.True(t, native.IdentityTruncated)
	require.True(t, utf8.ValidString(native.Name))
	require.LessOrEqual(t, utf8.RuneCountInString(native.Name), maxPayloadToolIdentityLen)
	require.Contains(t, native.Name, "characters truncated")
	mcp := payloadTool("", identity, identity)
	require.True(t, mcp.IdentityTruncated)
	require.True(t, utf8.ValidString(mcp.MCPServer))
	require.True(t, utf8.ValidString(mcp.MCPFunction))
	require.LessOrEqual(t, utf8.RuneCountInString(mcp.MCPServer), maxPayloadToolIdentityLen)
	require.LessOrEqual(t, utf8.RuneCountInString(mcp.MCPFunction), maxPayloadToolIdentityLen)
}
