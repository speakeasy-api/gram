package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// singleUpstreamToken collapses the per-remote-issuer token map for the
// single-Authorization remote-MCP backend. These cover the branches that the
// DB-backed resolver tests cannot reach while the one_per_issuer index still
// caps a user_session_issuer at one remote client.

func TestSingleUpstreamToken_EmptyMapReturnsEmpty(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(nil)
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestSingleUpstreamToken_SingleEntryReturnsToken(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(map[uuid.UUID]string{uuid.New(): "upstream-token"})
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token)
}

func TestSingleUpstreamToken_MultipleEntriesFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(map[uuid.UUID]string{
		uuid.New(): "token-a",
		uuid.New(): "token-b",
	})
	require.Error(t, err)
	require.Empty(t, token)
}
