package mcp_test

import (
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestConsentAgentEligibilityListAndSubmit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		delegate     bool
		authorize    bool
		ownerConnect bool
		agentConnect bool
		eligible     bool
	}{
		{name: "owner without management grants", ownerConnect: true, agentConnect: true, eligible: true},
		{name: "permitted delegate without agent read", delegate: true, authorize: true, ownerConnect: true, agentConnect: true, eligible: true},
		{name: "unauthorized nonowner", delegate: true, ownerConnect: true, agentConnect: true},
		{name: "owner connect cap", agentConnect: true},
		{name: "delegate cannot bypass owner connect cap", delegate: true, authorize: true, agentConnect: true},
		{name: "owner cannot bypass direct agent policy", ownerConnect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			fx := newAgentConsentFixture(t, ctx, ti)
			// Fresh members have no management grants, including agent:read.
			ownerID := seedConsentMember(t, ctx, ti, fx.orgID)
			ownerFixture := fx
			ownerFixture.userID = ownerID
			agent := createConsentAgent(t, ctx, ti, ownerFixture, "Eligibility regression agent")
			if tc.ownerConnect {
				seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, ownerID), fx.target.MCPResourceID)
			}
			if tc.agentConnect {
				seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
			}
			authorizerID := ownerID
			if tc.delegate {
				authorizerID = seedConsentMember(t, ctx, ti, fx.orgID)
				// The delegate's own runtime access must not substitute for the owner's.
				seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, authorizerID), fx.target.MCPResourceID)
			}
			if tc.authorize {
				seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, authorizerID), authz.ScopeAgentAuthorize, agent.ID.String())
			}
			state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
			require.NoError(t, err)
			subject := urn.NewUserSubject(authorizerID)
			state.AuthorizerUserID = authorizerID
			state.Subject = &subject
			require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

			w := serveAgentConsentGet(t, ctx, ti, fx)
			require.Equal(t, http.StatusOK, w.Code)
			if tc.eligible {
				require.Contains(t, w.Body.String(), agent.Name)
			} else {
				require.NotContains(t, w.Body.String(), agent.Name)
			}
			w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
			if tc.eligible {
				require.NoError(t, err)
				require.Equal(t, http.StatusSeeOther, w.Code)
			} else {
				require.Error(t, err)
				var shareable *oops.ShareableError
				require.ErrorAs(t, err, &shareable)
				require.Equal(t, oops.CodeForbidden, shareable.Code)
				require.Empty(t, w.Header().Get("Location"))
			}
		})
	}
}

func TestConsentAgentEligibilityListKeepsOwnerCapsSeparate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	authorizerID := seedConsentMember(t, ctx, ti, fx.orgID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, authorizerID), fx.target.MCPResourceID)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	subject := urn.NewUserSubject(authorizerID)
	state.AuthorizerUserID = authorizerID
	state.Subject = &subject
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

	allowedOwnerID := seedConsentMember(t, ctx, ti, fx.orgID)
	deniedOwnerID := seedConsentMember(t, ctx, ti, fx.orgID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, allowedOwnerID), fx.target.MCPResourceID)
	for _, tc := range []struct {
		owner string
		name  string
	}{
		{allowedOwnerID, "Allowed owner first agent"},
		{allowedOwnerID, "Allowed owner second agent"},
		{deniedOwnerID, "Denied owner agent"},
	} {
		ownerFixture := fx
		ownerFixture.userID = tc.owner
		agent := createConsentAgent(t, ctx, ti, ownerFixture, tc.name)
		seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, authorizerID), authz.ScopeAgentAuthorize, agent.ID.String())
		seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	}
	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Allowed owner first agent")
	require.Contains(t, w.Body.String(), "Allowed owner second agent")
	require.NotContains(t, w.Body.String(), "Denied owner agent")
}
