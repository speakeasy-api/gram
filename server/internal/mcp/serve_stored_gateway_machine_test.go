package mcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServePublic_StoredGatewayRolloutDoesNotChangePeople(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	gatewaySlug := "person-stored-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewaySlug, issuerID)
	memberSlug := "person-member-" + uuid.NewString()[:8]
	memberID := seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "person member", memberSlug, 0, mcpservers.VisibilityPrivate)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, memberID.String())
	token := mintEndpointUserSession(t, ti, issuerID, gatewaySlug, urn.NewUserSubject(authCtx.UserID))
	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "list_servers", "arguments": map[string]any{}})

	before, err := servePublicHTTP(t, ctx, ti, gatewaySlug, body, token, nil)
	require.NoError(t, err)
	ti.features.SetFlag(feature.FlagStoredGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	after, err := servePublicHTTP(t, ctx, ti, gatewaySlug, body, token, nil)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	require.Equal(t, before.Body.String(), after.Body.String())
	require.Contains(t, after.Body.String(), memberSlug)
}

func TestServePublic_StoredGatewayRejectsAgentKeyScopedToAnotherProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	gatewaySlug := "cross-project-stored-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewaySlug, issuerID)
	memberID := seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "private member", "private-"+uuid.NewString()[:8], 0, mcpservers.VisibilityPrivate)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Cross-project gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, memberID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), memberID)
	token := mintAgentGatewayKeyForProject(t, ctx, ti, agent.ID, uuid.New(), memberID)
	ti.features.SetFlag(feature.FlagStoredGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	_, err = servePublicHTTP(t, ctx, ti, gatewaySlug, makeInitializeBody(), token, nil)

	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServePublic_StoredGatewayAgentPrincipalCredentialInitializes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	gatewaySlug := "principal-stored-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewaySlug, issuerID)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Stored gateway principal " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedPrincipalGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeProjectRead, authCtx.ProjectID.String())
	token, _, err := ti.principalCredentials.Mint(principalcredential.Credential{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Principal:        urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		AuthorizerUserID: authCtx.UserID,
	})
	require.NoError(t, err)
	ti.features.SetFlag(feature.FlagStoredGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	w, err := servePublicHTTP(t, ctx, ti, gatewaySlug, makeInitializeBody(), token, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestServePublic_StoredGatewayIssuerWorkloadListsAdmittedMember(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := f.fixtureContext(t)
	issuerURL := f.sharedIssuer(t)

	gatewaySlug := "workload-stored-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.fx.target.ProjectID, f.fx.orgID, gatewaySlug, f.fx.target.UserSessionIssuerID)
	memberSlug := "workload-member-" + uuid.NewString()[:8]
	upstream := newRecordingUpstream(t, "ping")
	member := createMetaMemberWithVisibility(t, ctx, f.ti.conn, f.fx.target.ProjectID, gateway.ID, memberSlug, upstream.url, uuid.NullUUID{}, 0, mcpservers.VisibilityPrivate)
	memberID := member.mcpServerID
	seedPrincipalMCPConnectGrant(t, ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), memberID)

	tokenResponse := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, tokenResponse.Code, tokenResponse.Body.String())
	bearer := decodeSharedTokenResponse(t, tokenResponse).AccessToken
	require.NotEmpty(t, bearer)

	_, err := servePublicHTTP(t, t.Context(), f.ti, gatewaySlug, makeInitializeBody(), bearer, nil)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	f.ti.features.SetFlag(feature.FlagStoredGatewayMachineAccess, f.fx.orgID, true)

	w, err := servePublicHTTP(t, t.Context(), f.ti, gatewaySlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}), bearer, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), memberSlug)

	call, err := servePublicHTTP(t, t.Context(), f.ti, gatewaySlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": "execute_tool",
		"arguments": map[string]any{
			"name": memberSlug + "--ping", "arguments": map[string]any{},
		},
	}), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, call.Code, call.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, call))
	require.False(t, isError, text)
	require.Contains(t, text, "pong from ping")
}

func TestServePublic_StoredGatewayAgentListsOnlyAdmittedMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	gatewaySlug := "agent-stored-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewaySlug, issuerID)
	grantedSlug := "granted-" + uuid.NewString()[:8]
	deniedSlug := "denied-" + uuid.NewString()[:8]
	publicSlug := "public-" + uuid.NewString()[:8]
	upstream := newRecordingUpstream(t, "ping")
	granted := createMetaMemberWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, grantedSlug, upstream.url, uuid.NullUUID{}, 0, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "denied member", deniedSlug, 1, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "public member", publicSlug, 2, mcpservers.VisibilityPublic)
	grantedServerID := granted.mcpServerID

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Stored gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedServerID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), grantedServerID)
	token := mintAgentGatewayKey(t, ctx, ti, agent.ID, grantedServerID)
	ti.features.SetFlag(feature.FlagStoredGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	w, err := servePublicHTTP(t, ctx, ti, gatewaySlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}), token, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), grantedSlug)
	require.NotContains(t, w.Body.String(), deniedSlug)
	require.Contains(t, w.Body.String(), publicSlug)

	call, err := servePublicHTTP(t, ctx, ti, gatewaySlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": "execute_tool",
		"arguments": map[string]any{
			"name": grantedSlug + "--ping", "arguments": map[string]any{},
		},
	}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, call.Code, call.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, call))
	require.False(t, isError, text)
	require.Contains(t, text, "pong from ping")

	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, grantedServerID.String()).MarshalJSON()
	require.NoError(t, err)
	removed, err := accessrepo.New(ti.conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	revoked, err := servePublicHTTP(t, ctx, ti, gatewaySlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}), token, nil)
	require.NoError(t, err)
	require.NotContains(t, revoked.Body.String(), grantedSlug)
	require.Contains(t, revoked.Body.String(), publicSlug)
}
