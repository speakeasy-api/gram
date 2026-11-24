package mcp

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAgentProjectBoundaryShadowStateRecordsEachServerOnce(t *testing.T) {
	t.Parallel()
	state := &agentProjectBoundaryShadowState{mu: sync.Mutex{}, recorded: make(map[uuid.UUID]struct{})}
	server := uuid.New()

	require.True(t, state.first(server))
	require.False(t, state.first(server))
	require.True(t, state.first(uuid.New()))
}
