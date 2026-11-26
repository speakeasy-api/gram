package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestHostedGatewayTokenCacheIsolatesServerAndProject(t *testing.T) {
	t.Parallel()
	project, other, issuer := uuid.New(), uuid.New(), uuid.New()
	first := metaMember{serverID: uuid.New(), projectID: project, backend: metaMemberBackendHosted}
	sibling := metaMember{serverID: uuid.New(), projectID: project, backend: metaMemberBackendHosted}
	wrongProject := metaMember{serverID: first.serverID, projectID: other, backend: metaMemberBackendHosted}
	calls := 0
	gate := metaGateContext{projectTokens: &projectTokenCache{
		resolveMember: func(_ context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			calls++
			if member.projectID != project {
				return nil, remotesessions.ErrRemoteSessionMisconfigured
			}
			return map[uuid.UUID]remotesessions.UpstreamToken{issuer: {Token: member.serverID.String()}}, nil
		},
	}}
	for _, member := range []metaMember{first, sibling, first} {
		tokens, err := gate.memberTokens(t.Context(), member)
		require.NoError(t, err)
		require.Equal(t, member.serverID.String(), tokens[issuer].Token)
	}
	_, err := gate.memberTokens(t.Context(), wrongProject)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	require.Equal(t, 3, calls)
}
