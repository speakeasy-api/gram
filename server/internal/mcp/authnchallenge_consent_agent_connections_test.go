package mcp_test

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConsentAgentRequiresConnectionsBeforeConsumingChallenge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Connection agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	createConsentRemoteClient(t, ctx, ti.conn, fx.target.ProjectID, fx.orgID, "consent-required", "", []uuid.UUID{fx.target.UserSessionIssuerID})
	_, err := serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	require.Equal(t, fx.stateID, state.ID)
}
