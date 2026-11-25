package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestServeProjectGateway_WorkloadMachineAccess(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)
	// Use only private members to test exchange before any member is assigned.
	fixture := seedOrganizationGatewayWithOpenMemberVisibility(t, f.ctx, f.ti, mcpservers.VisibilityPrivate)
	meta := createMetaMcpEndpoint(t, f.ctx, f.ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "decoy-"+uuid.NewString(), uuid.Nil)
	member := seedHostedMetaMember(t, f.ctx, f.ti, meta.ID, "private hosted member", 0, mcpservers.VisibilityPrivate, "alpha_tool")
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, member.toolsetID.String())
	_, err := testrepo.New(f.ti.conn).SetUserSessionIssuerAuthorizationServerModeFixture(f.ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		OrganizationID: authCtx.ActiveOrganizationID, IssuerID: fixture.issuerID, AuthorizationServerMode: "shared",
	})
	require.NoError(t, err)
	root := strings.TrimSuffix(f.ti.serverURL.String(), "/") + "/mcp/gateway/" + fixture.organizationSlug + "/projects/" + fixture.projectSlug

	_, ok, err = f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.False(t, ok)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, f.fx.orgID, true)
	target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.True(t, ok)
	issuer, err := usersessionsrepo.New(f.ti.conn).GetSharedUserSessionIssuerByID(f.ctx, fixture.issuerID)
	require.NoError(t, err)
	shared, err := (authserver.Hosts{ServerURL: f.ti.serverURL.String()}).SharedAuthorizationServer(issuer.UserSessionIssuer)
	require.NoError(t, err)
	require.Equal(t, shared.TokenEndpoint, target.TokenEndpoint)
	require.Equal(t, shared.TokenEndpoint, target.Audience)
	require.Equal(t, root, target.Resource)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, target.Audience)},
		oauthwire.ParamResource:  {target.Resource},
	}
	router := goahttp.NewMuxer()
	mcp.Attach(router, f.ti.service.Service, nil)
	request := httptest.NewRequest(http.MethodPost, target.TokenEndpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	bearer := decodeSharedTokenResponse(t, response).AccessToken
	require.NotEmpty(t, bearer)

	requestGateway := func(body []byte) *httptest.ResponseRecorder {
		t.Helper()
		res, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		return res
	}
	list := func() string {
		t.Helper()
		body := requestGateway(listServersBody(t)).Body.String()
		require.NotContains(t, body, fixture.denied)
		require.NotContains(t, body, fixture.open)
		return body
	}
	toolList := makeMetaRPCBody(t, "tools/list", map[string]any{})
	contract := decodeRPCResponse(t, requestGateway(toolList))["result"]
	for _, name := range []string{metamcp.ToolListServers, metamcp.ToolDescribeServer, metamcp.ToolDescribeTools, metamcp.ToolExecuteTool, metamcp.ToolConnectionStatus} {
		require.Contains(t, string(contract), `"name":"`+name+`"`)
	}
	toolName := member.slug + "--alpha_tool"
	require.NotContains(t, string(contract), toolName, "member tools are not top-level gateway tools")
	require.NotContains(t, list(), member.slug, "the owner's grant alone is insufficient")
	grantID := seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), member.toolsetID)
	require.Contains(t, list(), member.slug)
	require.JSONEq(t, string(contract), string(decodeRPCResponse(t, requestGateway(toolList))["result"]))
	describe := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolDescribeServer, "arguments": map[string]any{"server": member.slug}})
	require.Contains(t, requestGateway(describe).Body.String(), toolName)
	execute := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolExecuteTool, "arguments": map[string]any{"name": toolName, "arguments": map[string]any{}}})
	// The fixture tool has no upstream URL; this error proves the call reached its dispatch.
	text, isError := metaToolResultText(t, decodeRPCResponse(t, requestGateway(execute)))
	require.True(t, isError)
	require.Contains(t, text, "no server URL")
	_, err = accessrepo.New(f.ti.conn).DeletePrincipalGrant(f.ctx, accessrepo.DeletePrincipalGrantParams{OrganizationID: authCtx.ActiveOrganizationID, ID: grantID})
	require.NoError(t, err)
	require.NotContains(t, list(), member.slug)
	require.Contains(t, requestGateway(describe).Body.String(), "unknown server")
	require.Contains(t, requestGateway(execute).Body.String(), "unknown server")
	require.JSONEq(t, string(contract), string(decodeRPCResponse(t, requestGateway(toolList))["result"]))

	_, err = serveOrganizationGatewayHTTP(t, f.ctx, f.ti, fixture.organizationSlug, bearer, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServeProjectGateway_AgentMachineAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	fixture := seedOrganizationGateway(t, ctx, ti)
	grantedID := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fixture.granted)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Project gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), grantedID)
	token := mintAgentGatewayKey(t, ctx, ti, agent.ID, grantedID)

	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	anonymousToken := mintProjectGatewayBearerForSubject(t, ti, fixture, fixture.projectSlug, urn.NewAnonymousSubject(uuid.NewString()))
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, anonymousToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	assistantToken := mintAssistantBearerForOwner(t, ti, authCtx)
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, assistantToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), fixture.granted)
	require.NotContains(t, listed.Body.String(), fixture.denied)
	require.Contains(t, listed.Body.String(), fixture.open)

	_, err = serveOrganizationGatewayHTTP(t, ctx, ti, fixture.organizationSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}
