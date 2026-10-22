package mcp

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"

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

func TestCodeMemberSessionPoolRequiresCallerScope(t *testing.T) {
	t.Parallel()
	pool := newCodeMemberSessions(nil, 0, 0, 0, 1)
	for _, scope := range []*codeSessionScope{nil, {}, {callerScope: nil}} {
		_, err := newCodeMemberSessionKey(scope, "server")
		require.ErrorContains(t, err, "requires caller scope and destination")
	}
	_, err := pool.acquire(t.Context(), codeMemberSessionKey{}, nil, nil)
	require.ErrorContains(t, err, "requires caller scope and destination")
	require.Empty(t, pool.entries, "an invalid scope never installs a destination-only entry")
}

func TestCodeMemberSessionCallerScopeIgnoresGrantOrder(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{{PrincipalUrn: "user:example", Scope: "mcp:connect", Selector: authz.Selector{"project": "a"}}, {PrincipalUrn: "user:example", Scope: "mcp:tool:call", Selector: authz.Selector{"project": "b"}}}
	makeBackend := func(grants []authz.Grant) *metaCodeBackend {
		ctx := authz.GrantsToContext(t.Context(), grants)
		ctx = context.WithValue(ctx, metaCodeCancellableKey{}, true)
		backend, err := newMetaCodeBackend(ctx, &Service{codeSessions: newCodeMemberSessions(nil, 0, 0, 0, 1)}, &metaGateContext{authenticated: true}, nil, endpointrepo.McpEndpoint{}, uuid.NullUUID{}, nil, nil)
		require.NoError(t, err)
		return backend
	}
	first := makeBackend(grants)
	second := makeBackend([]authz.Grant{grants[1], grants[0]})
	require.Equal(t, first.sessions.callerScope, second.sessions.callerScope)
	key, err := newCodeMemberSessionKey(first.sessions, "server")
	require.NoError(t, err)
	require.NotEmpty(t, key.callerScope.key)
}
