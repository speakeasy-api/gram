package mcp

import (
	"testing"

	"github.com/google/uuid"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/stretchr/testify/require"
)

func TestCodeBridgeAdmissionAllowsAnonymousPublicScope(t *testing.T) {
	t.Parallel()
	backend, err := newMetaCodeBackend(t.Context(), &Service{}, &metaGateContext{}, nil, endpointrepo.McpEndpoint{}, uuid.NullUUID{}, nil, nil)
	require.NoError(t, err, "anonymous member admission requires no user grant boundary")
	require.Empty(t, backend.admitted)
	require.False(t, backend.gate.authenticated)
}
